package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"gateway/Internal/session"

	"github.com/google/uuid"
	acp "github.com/ironpark/go-acp"
	"resty.dev/v3"
)

const maxSandboxErrorBody = 512

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var _ acp.Client = (*SouthHandler)(nil)
var _ acp.ExtMethodHandler = (*SouthHandler)(nil)

// SouthHandler handles callbacks from agent-side connections. Session updates
// and permission prompts go north; file and terminal work stays in the sandbox.
type SouthHandler struct {
	manager SessionManager
	logger  *slog.Logger
	http    *resty.Client

	mu        sync.Mutex
	terminals map[string]*terminalJob
}

type terminalJob struct {
	cancel context.CancelFunc
	done   chan struct{}

	output    string
	truncated bool
	exitCode  *int64
	err       error
}

func NewSouthHandler(ctx context.Context, manager SessionManager) *SouthHandler {
	return &SouthHandler{
		manager:   manager,
		logger:    slog.Default().With("component", "south-handler"),
		http:      resty.New().SetTimeout(180 * time.Second),
		terminals: make(map[string]*terminalJob),
	}
}

func (h *SouthHandler) SessionUpdate(ctx context.Context, params *acp.SessionNotification) error {
	conn, err := h.northConn(params.SessionID)
	if err != nil {
		return err
	}

	params.SessionID = conn.NorthID
	if agentID := session.AgentIDFromContext(ctx); agentID != "" {
		if params.Meta == nil {
			params.Meta = make(map[string]any)
		}
		params.Meta[session.MetaAgentID] = string(agentID)
	}

	return conn.NorthConn.SessionUpdate(ctx, params)
}

func (h *SouthHandler) RequestPermission(ctx context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	conn, err := h.northConn(params.SessionID)
	if err != nil {
		return nil, err
	}
	params.SessionID = conn.NorthID
	return conn.NorthConn.RequestPermission(ctx, params)
}

func (h *SouthHandler) ReadTextFile(ctx context.Context, params *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {

	target, err := h.sandboxURL(ctx, params.SessionID, "download/"+url.PathEscape(params.Path))
	if err != nil {
		return nil, err
	}

	resp, err := h.http.R().
		SetContext(ctx).
		Get(target)
	if err != nil {
		return nil, err
	}
	if err := sandboxResponseError(resp); err != nil {
		return nil, err
	}

	content := sliceText(resp.String(), params.Line, params.Limit)
	return &acp.ReadTextFileResponse{Content: content}, nil
}

func (h *SouthHandler) WriteTextFile(ctx context.Context, params *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	if params.Path == "" {
		return nil, errors.New("path is required")
	}

	command := writeFileCommand(params.Path, params.Content)
	if _, err := h.runSandboxCommand(ctx, params.SessionID, command); err != nil {
		return nil, err
	}

	return &acp.WriteTextFileResponse{}, nil
}

func (h *SouthHandler) CreateTerminal(ctx context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	if _, err := h.sandboxEndpoint(ctx, params.SessionID); err != nil {
		return nil, err
	}

	command, err := terminalCommand(params)
	if err != nil {
		return nil, err
	}

	terminalID := uuid.NewString()
	runCtx, cancel := context.WithCancel(context.Background())
	job := &terminalJob{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	h.mu.Lock()
	h.terminals[terminalID] = job
	h.mu.Unlock()

	go func() {
		defer close(job.done)
		result, err := h.runSandboxCommand(runCtx, params.SessionID, command)

		var (
			output    string
			truncated bool
			exitCode  *int64
		)
		if result != nil {
			output = result.Stdout
			if result.Stderr != "" {
				output += result.Stderr
			}

			var limit int64
			if params.OutputByteLimit != nil {
				limit = *params.OutputByteLimit
			}
			output, truncated = limitOutput(output, limit)

			code := int64(result.ExitCode)
			exitCode = &code
		}

		h.mu.Lock()
		job.output = output
		job.truncated = truncated
		job.exitCode = exitCode
		job.err = err
		h.mu.Unlock()
	}()

	return &acp.CreateTerminalResponse{TerminalID: terminalID}, nil
}

func (h *SouthHandler) TerminalOutput(ctx context.Context, params *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	job, err := h.terminal(params.TerminalID)
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	output := job.output
	truncated := job.truncated
	exitCode := job.exitCode
	jobErr := job.err
	h.mu.Unlock()

	select {
	case <-job.done:
		if jobErr != nil {
			return nil, jobErr
		}
	default:
	}

	return &acp.TerminalOutputResponse{
		Output:     output,
		Truncated:  truncated,
		ExitStatus: terminalExitStatus(exitCode),
	}, nil
}

func (h *SouthHandler) ReleaseTerminal(ctx context.Context, params *acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
	h.mu.Lock()
	job := h.terminals[params.TerminalID]
	delete(h.terminals, params.TerminalID)
	h.mu.Unlock()

	if job != nil {
		job.cancel()
	}

	return &acp.ReleaseTerminalResponse{}, nil
}

func (h *SouthHandler) WaitForTerminalExit(ctx context.Context, params *acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
	job, err := h.terminal(params.TerminalID)
	if err != nil {
		return nil, err
	}

	select {
	case <-job.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	h.mu.Lock()
	exitCode := job.exitCode
	jobErr := job.err
	h.mu.Unlock()
	if jobErr != nil {
		return nil, jobErr
	}

	return &acp.WaitForTerminalExitResponse{ExitCode: exitCode}, nil
}

func (h *SouthHandler) KillTerminalCommand(ctx context.Context, params *acp.KillTerminalRequest) (*acp.KillTerminalResponse, error) {
	job, err := h.terminal(params.TerminalID)
	if err != nil {
		return nil, err
	}
	job.cancel()
	return &acp.KillTerminalResponse{}, nil
}

func (h *SouthHandler) ExtMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	raw := make(map[string]any)
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil, err
	}

	sessionID, _ := raw["sessionId"].(string)
	if sessionID == "" {
		return nil, errors.New("missing session id")
	}

	conn, err := h.northConn(acp.SessionID(sessionID))
	if err != nil {
		return nil, err
	}

	raw["sessionId"] = string(conn.NorthID)
	if agentID := session.MetaString(raw, session.MetaAgentID); agentID != "" {
		agentConn, err := h.manager.FindAgentConnection(conn.NorthID, session.AgentID(agentID))
		if err != nil {
			return nil, err
		}
		return agentConn.ExtMethod(ctx, method, raw)
	}

	return conn.NorthConn.ExtMethod(ctx, method, raw)
}

func (h *SouthHandler) northConn(sessionID acp.SessionID) (*session.Conn, error) {
	conn, err := h.manager.FindByNorth(sessionID)
	if err != nil {
		return nil, err
	}
	if conn.NorthConn == nil {
		return nil, errors.New("missing north connection")
	}
	return conn, nil
}

func (h *SouthHandler) runSandboxCommand(ctx context.Context, sessionID acp.SessionID, command string) (*sandboxExecutionResult, error) {
	target, err := h.sandboxURL(ctx, sessionID, "execute")
	if err != nil {
		return nil, err
	}

	var result sandboxExecutionResult
	resp, err := h.http.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"command": command}).
		SetResult(&result).
		Post(target)
	if err != nil {
		return nil, err
	}
	if err := sandboxResponseError(resp); err != nil {
		return nil, err
	}
	return &result, nil
}

func (h *SouthHandler) sandboxURL(ctx context.Context, sessionID acp.SessionID, resource string) (string, error) {
	endpoint, err := h.sandboxEndpoint(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return endpoint + "/" + strings.TrimLeft(resource, "/"), nil
}

func (h *SouthHandler) sandboxEndpoint(ctx context.Context, sessionID acp.SessionID) (string, error) {
	endpoint, err := h.manager.ResolveSandboxEndpoint(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if endpoint == "" {
		return "", errors.New("sandbox endpoint not established")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	return strings.TrimRight(endpoint, "/"), nil
}

func (h *SouthHandler) terminal(terminalID string) (*terminalJob, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	job := h.terminals[terminalID]
	if job == nil {
		return nil, errors.New("terminal not found")
	}
	return job, nil
}

func terminalExitStatus(exitCode *int64) *acp.TerminalExitStatus {
	if exitCode == nil {
		return nil
	}
	return &acp.TerminalExitStatus{ExitCode: exitCode}
}

type sandboxExecutionResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func sandboxResponseError(resp *resty.Response) error {
	if resp.IsSuccess() {
		return nil
	}

	body := resp.String()
	if len(body) > maxSandboxErrorBody {
		body = body[:maxSandboxErrorBody]
	}

	return fmt.Errorf("sandbox returned %d: %s", resp.StatusCode(), body)
}

func writeFileCommand(filePath, content string) string {
	dir := path.Dir(filePath)
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	command := fmt.Sprintf("printf %%s %s | base64 -d > %s", shellQuote(encoded), shellQuote(filePath))
	if dir != "." && dir != "/" {
		command = fmt.Sprintf("mkdir -p %s && %s", shellQuote(dir), command)
	}
	return command
}

func terminalCommand(params *acp.CreateTerminalRequest) (string, error) {
	command := params.Command
	if len(params.Args) > 0 {
		parts := make([]string, 0, len(params.Args)+1)
		parts = append(parts, shellQuote(params.Command))
		for _, arg := range params.Args {
			parts = append(parts, shellQuote(arg))
		}
		command = strings.Join(parts, " ")
	}

	env := make([]string, 0, len(params.Env))
	for _, item := range params.Env {
		if !envNamePattern.MatchString(item.Name) {
			return "", fmt.Errorf("invalid env name %q", item.Name)
		}
		env = append(env, item.Name+"="+shellQuote(item.Value))
	}
	if len(env) > 0 {
		command = strings.Join(env, " ") + " " + command
	}
	if params.Cwd != "" {
		command = "cd " + shellQuote(params.Cwd) + " && " + command
	}
	return command, nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func sliceText(content string, line, limit *int64) string {
	if line == nil && limit == nil {
		return content
	}

	lines := strings.SplitAfter(content, "\n")
	start := 0
	if line != nil && *line > 1 {
		start = int(*line - 1)
	}
	if start >= len(lines) {
		return ""
	}

	end := len(lines)
	if limit != nil && *limit >= 0 {
		end = min(end, start+int(*limit))
	}
	return strings.Join(lines[start:end], "")
}

func limitOutput(output string, limit int64) (string, bool) {
	if limit <= 0 || int64(len(output)) <= limit {
		return output, false
	}
	return output[:limit], true
}
