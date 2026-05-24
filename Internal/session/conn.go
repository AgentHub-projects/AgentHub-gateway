package session

import (
	"errors"

	"github.com/ironpark/go-acp"
)

type endpoint string
type AgentID string

type Conn struct {
	ConnID     string
	NorthID    acp.SessionID
	NorthConn  *acp.AgentSideConnection
	LeaderConn *acp.ClientSideConnection
	WorkerConn map[AgentID]*acp.ClientSideConnection
	sandbox    endpoint
}

func (s *Conn) Close() error {
	var errs []error
	if s == nil {
		return nil
	}
	if s.NorthConn != nil {
		errs = append(errs, s.NorthConn.Close())
	}
	if s.LeaderConn != nil {
		errs = append(errs, s.LeaderConn.Close())
	}
	for _, c := range s.WorkerConn {
		if c == nil || c == s.LeaderConn {
			continue
		}
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}
