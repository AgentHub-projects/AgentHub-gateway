package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"gateway/Internal/session"
	"gateway/utils/config"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"gateway/pkg/constants"

	"github.com/ironpark/go-acp"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	sandboxv1alpha1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extensionsv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const claimHashSuffixLength = 12

type Resolver struct {
	client       client.Client
	namespace    string
	pollInterval time.Duration
	endpointPort int
}

func NewEndpointResolver(cfg config.Sandbox, clt client.Client) (*Resolver, error) {
	if clt == nil {
		defaultClient, err := newClient()
		if err != nil {
			return nil, err
		}
		clt = defaultClient
	}

	if cfg.Namespace == "" {
		return nil, errors.New("empty sandbox namespace")
	}
	return &Resolver{
		client:       clt,
		namespace:    cfg.Namespace,
		pollInterval: cfg.PollInterval,
		endpointPort: cfg.Port,
	}, nil
}

func (p *Resolver) Resolve(ctx context.Context, sessionID acp.SessionID, agentID session.AgentID, templateSelector string) (string, error) {
	selector, err := labels.Parse(templateSelector)
	if err != nil {
		return "", fmt.Errorf("parse template label selector: %w", err)
	}

	template, err := p.resolveTemplate(ctx, selector)
	if err != nil {
		return "", err
	}
	claimName := makeClaimName(template.Name, string(sessionID), string(agentID))

	if err := p.ensureClaim(ctx, string(sessionID), string(agentID), claimName, template.Name); err != nil {
		return "", err
	}

	var address string
	condition := func(ctx context.Context) (bool, error) {
		resolved, err := p.resolveEndpoint(ctx, claimName)
		if err != nil {
			return false, err
		}
		if resolved == "" {
			return false, nil
		}

		address = resolved

		return true, nil
	}

	if err := wait.PollUntilContextTimeout(ctx, p.pollInterval, 30*time.Second, true, condition); err != nil {
		return "", fmt.Errorf("wait sandbox endpoint for claim %s: %w", claimName, err)
	}

	if address == "" {
		return "", fmt.Errorf("wait sandbox endpoint for claim %s", claimName)
	}

	return address, nil
}

func (p *Resolver) Release(ctx context.Context, sessionID acp.SessionID, agentID session.AgentID) error {
	var claims extensionsv1alpha1.SandboxClaimList
	if err := p.client.List(
		ctx,
		&claims,
		client.InNamespace(p.namespace),
		client.MatchingLabels{
			constants.LabelSessionID: string(sessionID),
			constants.LabelAgentID:   string(agentID),
		},
	); err != nil {
		return fmt.Errorf("list sandbox claims: %w", err)
	}

	var errs []error
	for i := range claims.Items {
		claim := &claims.Items[i]
		slog.Info("delete sandbox claim", "claim", claim.Name, "session", sessionID, "agent", agentID)
		if err := p.client.Delete(ctx, claim); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete sandbox claim %s: %w", claim.Name, err))
			continue
		}
		slog.Info("sandbox claim deleted", "claim", claim.Name, "session", sessionID, "agent", agentID)
	}

	return errors.Join(errs...)
}

func makeClaimName(templateName, sessionID, agentID string) string {
	templateName = strings.Trim(templateName, "-")
	if templateName == "" {
		templateName = "sandbox"
	}

	hash := sha256.Sum256([]byte(strings.Join([]string{templateName, sessionID, agentID}, "\x00")))
	suffix := hex.EncodeToString(hash[:])[:claimHashSuffixLength]

	maxPrefixLen := 63 - 1 - claimHashSuffixLength
	if len(templateName) > maxPrefixLen {
		templateName = strings.Trim(templateName[:maxPrefixLen], "-")
	}

	return fmt.Sprintf("%s-%s", templateName, suffix)
}

func (p *Resolver) resolveTemplate(ctx context.Context, templateSelector labels.Selector) (*extensionsv1alpha1.SandboxTemplate, error) {
	var (
		list     extensionsv1alpha1.SandboxTemplateList
		selector = client.MatchingLabelsSelector{Selector: templateSelector}
	)

	if err := p.client.List(ctx, &list, client.InNamespace(p.namespace), selector); err != nil {
		return nil, fmt.Errorf("list sandbox templates: %w", err)
	}

	if len(list.Items) == 0 {
		return nil, fmt.Errorf("sandbox template selector %q matched no templates", templateSelector.String())
	}
	return &list.Items[0], nil
}

func (p *Resolver) ensureClaim(ctx context.Context, sessionID, agentID, claimName, templateName string) error {
	key := client.ObjectKey{Namespace: p.namespace, Name: claimName}

	var existing extensionsv1alpha1.SandboxClaim
	if err := p.client.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get sandbox claim %s: %w", claimName, err)
		}
		slog.Info("create sandbox claim")
		claim := &extensionsv1alpha1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: p.namespace,
				Labels: map[string]string{
					constants.LabelSessionID: sessionID,
					constants.LabelAgentID:   agentID,
				},
			},
			Spec: extensionsv1alpha1.SandboxClaimSpec{
				TemplateRef: extensionsv1alpha1.SandboxTemplateRef{Name: templateName},
				AdditionalPodMetadata: sandboxv1alpha1.PodMetadata{
					Labels: map[string]string{
						constants.LabelSessionID: sessionID,
						constants.LabelAgentID:   agentID,
					},
				},
			},
		}

		if err := p.client.Create(ctx, claim); err != nil {
			return fmt.Errorf("create sandbox claim %s: %w", claimName, err)
		}

		slog.Info("sandbox claim created")

		return nil
	}

	slog.Info("sandbox claim already exists")
	if owner := existing.Labels[constants.LabelSessionID]; owner != "" && owner != sessionID {
		return fmt.Errorf("sandbox claim %s belongs to session %s, not %s", claimName, owner, sessionID)
	}
	if owner := existing.Labels[constants.LabelAgentID]; owner != "" && owner != agentID {
		return fmt.Errorf("sandbox claim %s belongs to agent %s, not %s", claimName, owner, agentID)
	}
	if existing.Spec.TemplateRef.Name != templateName {
		return fmt.Errorf(
			"sandbox claim %s already points to template %s, want %s",
			claimName,
			existing.Spec.TemplateRef.Name,
			templateName,
		)
	}

	return nil
}

func (p *Resolver) resolveEndpoint(ctx context.Context, claimName string) (string, error) {
	var claim extensionsv1alpha1.SandboxClaim
	if err := p.client.Get(ctx, client.ObjectKey{Namespace: p.namespace, Name: claimName}, &claim); err != nil {
		return "", fmt.Errorf("get sandbox claim %s: %w", claimName, err)
	}

	sandboxName := claim.Status.SandboxStatus.Name
	if !isReady(claim.Status.Conditions) {
		return "", nil
	}

	var sandbox sandboxv1alpha1.Sandbox
	if err := p.client.Get(ctx, client.ObjectKey{Namespace: p.namespace, Name: sandboxName}, &sandbox); err != nil {
		return "", fmt.Errorf("get sandbox %s: %w", sandboxName, err)
	}

	serviceName := sandbox.Status.Service
	if serviceName == "" {
		var ok bool
		if serviceName, _, ok = strings.Cut(sandbox.Status.ServiceFQDN, "."); !ok || serviceName == "" {
			return "", nil
		}
	}

	var slices discoveryv1.EndpointSliceList
	if err := p.client.List(
		ctx,
		&slices,
		client.InNamespace(sandbox.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: serviceName},
	); err != nil {
		return "", fmt.Errorf("list endpointslices for service %s/%s: %w", sandbox.Namespace, serviceName, err)
	}

	ready := false
	for _, slice := range slices.Items {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}
			if len(endpoint.Addresses) == 0 {
				continue
			}
			ready = true
			break
		}
		if ready {
			break
		}
	}

	if !ready {
		return "", nil
	}

	host := sandbox.Status.ServiceFQDN
	if host == "" {
		host = fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, sandbox.Namespace)
	}

	return net.JoinHostPort(host, strconv.Itoa(int(p.endpointPort))), nil
}

func isReady(conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type == sandboxv1alpha1.SandboxConditionReady.String() {
			return condition.Status == metav1.ConditionTrue
		}
	}

	return false
}
