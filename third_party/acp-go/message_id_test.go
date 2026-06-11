package acp

import (
	"context"
	"testing"
)

// --- Constructor tests ---

func TestAgentMessageChunkWithoutMessageID(t *testing.T) {
	update := NewSessionUpdateAgentMessageChunk(NewContentBlockText("hello"), "")
	chunk, ok := update.AsAgentMessageChunk()
	if !ok {
		t.Fatal("Expected agent message chunk")
	}
	if chunk.MessageID != "" {
		t.Errorf("Expected empty messageID, got %q", chunk.MessageID)
	}
}

func TestAgentMessageChunkWithMessageID(t *testing.T) {
	update := NewSessionUpdateAgentMessageChunk(NewContentBlockText("hello"), "msg-1")
	chunk, ok := update.AsAgentMessageChunk()
	if !ok {
		t.Fatal("Expected agent message chunk")
	}
	if chunk.MessageID != "msg-1" {
		t.Errorf("Expected messageID %q, got %q", "msg-1", chunk.MessageID)
	}
}

func TestUserMessageChunkWithoutMessageID(t *testing.T) {
	update := NewSessionUpdateUserMessageChunk(NewContentBlockText("hi"), "")
	chunk, ok := update.AsUserMessageChunk()
	if !ok {
		t.Fatal("Expected user message chunk")
	}
	if chunk.MessageID != "" {
		t.Errorf("Expected empty messageID, got %q", chunk.MessageID)
	}
}

func TestUserMessageChunkWithMessageID(t *testing.T) {
	update := NewSessionUpdateUserMessageChunk(NewContentBlockText("hi"), "msg-2")
	chunk, ok := update.AsUserMessageChunk()
	if !ok {
		t.Fatal("Expected user message chunk")
	}
	if chunk.MessageID != "msg-2" {
		t.Errorf("Expected messageID %q, got %q", "msg-2", chunk.MessageID)
	}
}

func TestAgentThoughtChunkWithoutMessageID(t *testing.T) {
	update := NewSessionUpdateAgentThoughtChunk(NewContentBlockText("thinking"), "")
	chunk, ok := update.AsAgentThoughtChunk()
	if !ok {
		t.Fatal("Expected agent thought chunk")
	}
	if chunk.MessageID != "" {
		t.Errorf("Expected empty messageID, got %q", chunk.MessageID)
	}
}

func TestAgentThoughtChunkWithMessageID(t *testing.T) {
	update := NewSessionUpdateAgentThoughtChunk(NewContentBlockText("thinking"), "msg-3")
	chunk, ok := update.AsAgentThoughtChunk()
	if !ok {
		t.Fatal("Expected agent thought chunk")
	}
	if chunk.MessageID != "msg-3" {
		t.Errorf("Expected messageID %q, got %q", "msg-3", chunk.MessageID)
	}
}

// --- SessionStream tests ---

// mockClient captures SessionUpdate calls for inspection.
type mockClient struct {
	updates []*SessionNotification
}

func (m *mockClient) SessionUpdate(_ context.Context, params *SessionNotification) error {
	m.updates = append(m.updates, params)
	return nil
}

func (m *mockClient) RequestPermission(_ context.Context, _ *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return nil, nil
}

func (m *mockClient) ReadTextFile(_ context.Context, _ *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	return nil, nil
}

func (m *mockClient) WriteTextFile(_ context.Context, _ *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	return nil, nil
}

func (m *mockClient) CreateTerminal(_ context.Context, _ *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return nil, nil
}

func (m *mockClient) TerminalOutput(_ context.Context, _ *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return nil, nil
}

func (m *mockClient) ReleaseTerminal(_ context.Context, _ *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error) {
	return nil, nil
}

func (m *mockClient) WaitForTerminalExit(_ context.Context, _ *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	return nil, nil
}

func (m *mockClient) KillTerminalCommand(_ context.Context, _ *KillTerminalRequest) (*KillTerminalResponse, error) {
	return nil, nil
}

func TestSessionStreamWithoutMessageID(t *testing.T) {
	mc := &mockClient{}
	stream := NewSessionStream(mc, "sess-1")

	ctx := context.Background()
	if err := stream.SendText(ctx, "hello"); err != nil {
		t.Fatal(err)
	}

	if len(mc.updates) != 1 {
		t.Fatalf("Expected 1 update, got %d", len(mc.updates))
	}

	chunk, ok := mc.updates[0].Update.AsAgentMessageChunk()
	if !ok {
		t.Fatal("Expected agent message chunk")
	}
	if chunk.MessageID != "" {
		t.Errorf("Expected empty messageID, got %q", chunk.MessageID)
	}
}

func TestSessionStreamSendTextWithMessageID(t *testing.T) {
	mc := &mockClient{}
	stream := NewSessionStream(mc, "sess-1")

	ctx := context.Background()
	if err := stream.SendText(ctx, "hello", WithMessageID("msg-100")); err != nil {
		t.Fatal(err)
	}
	chunk, ok := mc.updates[0].Update.AsAgentMessageChunk()
	if !ok {
		t.Fatal("Expected agent message chunk")
	}
	if chunk.MessageID != "msg-100" {
		t.Errorf("Expected messageID %q, got %q", "msg-100", chunk.MessageID)
	}
}

func TestSessionStreamAllMethodsWithMessageID(t *testing.T) {
	mc := &mockClient{}
	stream := NewSessionStream(mc, "sess-1")

	ctx := context.Background()

	// SendText
	if err := stream.SendText(ctx, "hello", WithMessageID("msg-100")); err != nil {
		t.Fatal(err)
	}
	chunk, ok := mc.updates[0].Update.AsAgentMessageChunk()
	if !ok {
		t.Fatal("Expected agent message chunk")
	}
	if chunk.MessageID != "msg-100" {
		t.Errorf("SendText: expected messageID %q, got %q", "msg-100", chunk.MessageID)
	}

	// SendThought
	if err := stream.SendThought(ctx, "hmm", WithMessageID("msg-100")); err != nil {
		t.Fatal(err)
	}
	thought, ok := mc.updates[1].Update.AsAgentThoughtChunk()
	if !ok {
		t.Fatal("Expected agent thought chunk")
	}
	if thought.MessageID != "msg-100" {
		t.Errorf("SendThought: expected messageID %q, got %q", "msg-100", thought.MessageID)
	}

	// SendUserMessage
	if err := stream.SendUserMessage(ctx, "user says", WithMessageID("msg-100")); err != nil {
		t.Fatal(err)
	}
	userChunk, ok := mc.updates[2].Update.AsUserMessageChunk()
	if !ok {
		t.Fatal("Expected user message chunk")
	}
	if userChunk.MessageID != "msg-100" {
		t.Errorf("SendUserMessage: expected messageID %q, got %q", "msg-100", userChunk.MessageID)
	}

	// SendImage
	if err := stream.SendImage(ctx, "data", "image/png", "uri", WithMessageID("msg-100")); err != nil {
		t.Fatal(err)
	}
	imgChunk, ok := mc.updates[3].Update.AsAgentMessageChunk()
	if !ok {
		t.Fatal("Expected agent message chunk for image")
	}
	if imgChunk.MessageID != "msg-100" {
		t.Errorf("SendImage: expected messageID %q, got %q", "msg-100", imgChunk.MessageID)
	}
}

func TestSessionStreamEmptyMessageIDOmitted(t *testing.T) {
	mc := &mockClient{}
	stream := NewSessionStream(mc, "sess-1")

	ctx := context.Background()

	// First call with an ID
	if err := stream.SendText(ctx, "with id", WithMessageID("msg-200")); err != nil {
		t.Fatal(err)
	}

	// Second call without an ID
	if err := stream.SendText(ctx, "without id"); err != nil {
		t.Fatal(err)
	}

	chunk, _ := mc.updates[1].Update.AsAgentMessageChunk()
	if chunk.MessageID != "" {
		t.Errorf("Expected empty messageID, got %q", chunk.MessageID)
	}
}
