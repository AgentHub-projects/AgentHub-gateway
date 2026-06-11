package acp

import (
	"context"
	"testing"
)

func TestTerminalHandle(t *testing.T) {
	sessionID := SessionID("test-session")
	terminalID := "test-terminal"

	// Use TestClient as the terminalClient (it implements all terminal methods)
	client := &TestClient{}

	handle := NewTerminalHandle(terminalID, sessionID, client)

	if handle.ID != terminalID {
		t.Errorf("Expected terminal ID %s, got %s", terminalID, handle.ID)
	}

	if handle.sessionID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, handle.sessionID)
	}

	if handle.client != client {
		t.Error("Expected client to be set")
	}
}

func TestTerminalHandleMatchesTypeScriptAPI(t *testing.T) {
	client := &TestClient{}
	handle := NewTerminalHandle("test", SessionID("session"), client)

	// Verify all methods exist by checking their signatures compile
	_ = func(ctx context.Context) (*TerminalOutputResponse, error) {
		return handle.CurrentOutput(ctx)
	}

	_ = func(ctx context.Context) (*WaitForTerminalExitResponse, error) {
		return handle.WaitForExit(ctx)
	}

	_ = func(ctx context.Context) error {
		return handle.Kill(ctx)
	}

	_ = func(ctx context.Context) error {
		return handle.Release(ctx)
	}

	t.Log("All TypeScript-equivalent methods are implemented with correct signatures")
}
