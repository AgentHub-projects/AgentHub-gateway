package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConnection represents a bidirectional pipe similar to TypeScript's TransformStream
type TestConnection struct {
	clientToAgent *io.PipeReader
	agentToClient *io.PipeReader
	clientWriter  *io.PipeWriter
	agentWriter   *io.PipeWriter
}

func NewTestConnection() *TestConnection {
	clientReader, agentWriter := io.Pipe()
	agentReader, clientWriter := io.Pipe()

	return &TestConnection{
		clientToAgent: clientReader,
		agentToClient: agentReader,
		clientWriter:  clientWriter,
		agentWriter:   agentWriter,
	}
}

func (tc *TestConnection) Close() {
	tc.clientWriter.Close()
	tc.agentWriter.Close()
	tc.clientToAgent.Close()
	tc.agentToClient.Close()
}

// TestClient implements Client interface for integration testing
type TestClient struct {
	writeTextFileHandler     func(*WriteTextFileRequest) (*WriteTextFileResponse, error)
	readTextFileHandler      func(*ReadTextFileRequest) (*ReadTextFileResponse, error)
	requestPermissionHandler func(*RequestPermissionRequest) (*RequestPermissionResponse, error)
	sessionUpdateHandler     func(*SessionNotification) error
}

func (c *TestClient) WriteTextFile(ctx context.Context, params *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	if c.writeTextFileHandler != nil {
		return c.writeTextFileHandler(params)
	}
	return &WriteTextFileResponse{}, nil
}

func (c *TestClient) ReadTextFile(ctx context.Context, params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	if c.readTextFileHandler != nil {
		return c.readTextFileHandler(params)
	}
	return &ReadTextFileResponse{Content: "Mock file content"}, nil
}

func (c *TestClient) RequestPermission(ctx context.Context, params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	if c.requestPermissionHandler != nil {
		return c.requestPermissionHandler(params)
	}
	return &RequestPermissionResponse{
		Outcome: NewRequestPermissionOutcomeSelected(PermissionOptionID("allow")),
	}, nil
}

func (c *TestClient) SessionUpdate(ctx context.Context, params *SessionNotification) error {
	if c.sessionUpdateHandler != nil {
		return c.sessionUpdateHandler(params)
	}
	return nil
}

func (c *TestClient) CreateTerminal(ctx context.Context, params *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return &CreateTerminalResponse{TerminalID: "mock-terminal"}, nil
}

func (c *TestClient) TerminalOutput(ctx context.Context, params *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return &TerminalOutputResponse{Output: "mock output", Truncated: false}, nil
}

func (c *TestClient) ReleaseTerminal(ctx context.Context, params *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error) {
	return &ReleaseTerminalResponse{}, nil
}

func (c *TestClient) WaitForTerminalExit(ctx context.Context, params *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	exitCode := int64(0)
	return &WaitForTerminalExitResponse{ExitCode: &exitCode}, nil
}

func (c *TestClient) KillTerminalCommand(ctx context.Context, params *KillTerminalRequest) (*KillTerminalResponse, error) {
	return &KillTerminalResponse{}, nil
}

// TestAgent implements Agent interface for integration testing
type TestAgent struct {
	initializeHandler          func(*InitializeRequest) (*InitializeResponse, error)
	newSessionHandler          func(*NewSessionRequest) (*NewSessionResponse, error)
	loadSessionHandler         func(*LoadSessionRequest) (*LoadSessionResponse, error)
	listSessionsHandler        func(*ListSessionsRequest) (*ListSessionsResponse, error)
	setSessionModeHandler      func(*SetSessionModeRequest) (*SetSessionModeResponse, error)
	setSessionConfigHandler    func(*SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
	authenticateHandler        func(*AuthenticateRequest) (*AuthenticateResponse, error)
	promptHandler              func(*PromptRequest) (*PromptResponse, error)
	cancelHandler              func(*CancelNotification) error
}

func (a *TestAgent) Initialize(ctx context.Context, params *InitializeRequest) (*InitializeResponse, error) {
	if a.initializeHandler != nil {
		return a.initializeHandler(params)
	}
	return &InitializeResponse{
		ProtocolVersion:   ProtocolVersion(CurrentProtocolVersion),
		AgentCapabilities: &AgentCapabilities{LoadSession: false},
		AuthMethods:       []AuthMethod{},
	}, nil
}

func (a *TestAgent) NewSession(ctx context.Context, params *NewSessionRequest) (*NewSessionResponse, error) {
	if a.newSessionHandler != nil {
		return a.newSessionHandler(params)
	}
	return &NewSessionResponse{SessionID: SessionID("test-session")}, nil
}

func (a *TestAgent) LoadSession(ctx context.Context, params *LoadSessionRequest) (*LoadSessionResponse, error) {
	if a.loadSessionHandler != nil {
		return a.loadSessionHandler(params)
	}
	return &LoadSessionResponse{}, nil
}

func (a *TestAgent) ListSessions(ctx context.Context, params *ListSessionsRequest) (*ListSessionsResponse, error) {
	if a.listSessionsHandler != nil {
		return a.listSessionsHandler(params)
	}
	return &ListSessionsResponse{}, nil
}

func (a *TestAgent) SetSessionMode(ctx context.Context, params *SetSessionModeRequest) (*SetSessionModeResponse, error) {
	if a.setSessionModeHandler != nil {
		return a.setSessionModeHandler(params)
	}
	return &SetSessionModeResponse{}, nil
}

func (a *TestAgent) SetSessionConfigOption(ctx context.Context, params *SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	if a.setSessionConfigHandler != nil {
		return a.setSessionConfigHandler(params)
	}
	return &SetSessionConfigOptionResponse{ConfigOptions: []SessionConfigOption{}}, nil
}

func (a *TestAgent) Authenticate(ctx context.Context, params *AuthenticateRequest) (*AuthenticateResponse, error) {
	if a.authenticateHandler != nil {
		return a.authenticateHandler(params)
	}
	return &AuthenticateResponse{}, nil
}

func (a *TestAgent) Prompt(ctx context.Context, params *PromptRequest) (*PromptResponse, error) {
	if a.promptHandler != nil {
		return a.promptHandler(params)
	}
	return &PromptResponse{StopReason: StopReasonEndTurn}, nil
}

func (a *TestAgent) Cancel(ctx context.Context, params *CancelNotification) error {
	if a.cancelHandler != nil {
		return a.cancelHandler(params)
	}
	return nil
}

func TestBidirectionalCommunicationErrors(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	// Create client that throws errors
	testClient := &TestClient{
		writeTextFileHandler: func(*WriteTextFileRequest) (*WriteTextFileResponse, error) {
			return nil, fmt.Errorf("write failed")
		},
		readTextFileHandler: func(*ReadTextFileRequest) (*ReadTextFileResponse, error) {
			return nil, fmt.Errorf("read failed")
		},
		requestPermissionHandler: func(*RequestPermissionRequest) (*RequestPermissionResponse, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}

	// Create agent that throws errors
	testAgent := &TestAgent{
		initializeHandler: func(*InitializeRequest) (*InitializeResponse, error) {
			return nil, fmt.Errorf("failed to initialize")
		},
		newSessionHandler: func(req *NewSessionRequest) (*NewSessionResponse, error) {
			return nil, fmt.Errorf("failed to create session")
		},
		loadSessionHandler: func(*LoadSessionRequest) (*LoadSessionResponse, error) {
			return nil, fmt.Errorf("failed to load session")
		},
		authenticateHandler: func(*AuthenticateRequest) (*AuthenticateResponse, error) {
			return nil, fmt.Errorf("authentication failed")
		},
		promptHandler: func(*PromptRequest) (*PromptResponse, error) {
			return nil, fmt.Errorf("prompt failed")
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Test error handling in client->agent direction
	var writeErr error
	for range 3 {
		_, writeErr = clientConnection.Client().WriteTextFile(ctx, &WriteTextFileRequest{
			Path:      "/test.txt",
			Content:   "test",
			SessionID: SessionID("test-session"),
		})
		if writeErr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if writeErr == nil {
		t.Error("Expected error from WriteTextFile, got nil after 3 retries")
	} else if !strings.Contains(writeErr.Error(), "write failed") {
		t.Errorf("Expected error containing 'write failed', got: %v", writeErr)
	}

	// Test error handling in agent->client direction
	var sessionErr error
	for range 3 {
		_, sessionErr = agentConnection.NewSession(ctx, &NewSessionRequest{
			Cwd:        "/test",
			MCPServers: []MCPServer{},
		})
		if sessionErr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sessionErr == nil {
		t.Error("Expected error from NewSession, got nil after 3 retries")
	} else if !strings.Contains(sessionErr.Error(), "failed to create session") {
		t.Errorf("Expected error containing 'failed to create session', got: %v", sessionErr)
	}
}

func TestConcurrentRequests(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	var requestCount int64

	// Create client
	testClient := &TestClient{
		writeTextFileHandler: func(params *WriteTextFileRequest) (*WriteTextFileResponse, error) {
			atomic.AddInt64(&requestCount, 1)
			currentCount := atomic.LoadInt64(&requestCount)
			// Simulate work
			time.Sleep(40 * time.Millisecond)
			t.Logf("Write request %d completed", currentCount)
			return &WriteTextFileResponse{}, nil
		},
		readTextFileHandler: func(params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
			return &ReadTextFileResponse{Content: fmt.Sprintf("Content of %s", params.Path)}, nil
		},
		requestPermissionHandler: func(*RequestPermissionRequest) (*RequestPermissionResponse, error) {
			return &RequestPermissionResponse{
				Outcome: NewRequestPermissionOutcomeSelected(PermissionOptionID("allow")),
			}, nil
		},
	}

	// Create agent
	testAgent := &TestAgent{
		initializeHandler: func(*InitializeRequest) (*InitializeResponse, error) {
			return &InitializeResponse{
				ProtocolVersion:   ProtocolVersion(1),
				AgentCapabilities: &AgentCapabilities{LoadSession: false},
				AuthMethods:       []AuthMethod{},
			}, nil
		},
		newSessionHandler: func(*NewSessionRequest) (*NewSessionResponse, error) {
			return &NewSessionResponse{SessionID: SessionID("test-session")}, nil
		},
		promptHandler: func(*PromptRequest) (*PromptResponse, error) {
			return &PromptResponse{StopReason: StopReasonEndTurn}, nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Send multiple concurrent requests
	var wg sync.WaitGroup
	wg.Add(3)

	errors := make([]error, 3)

	for i := range 3 {
		go func(index int) {
			defer wg.Done()
			_, err := clientConnection.Client().WriteTextFile(ctx, &WriteTextFileRequest{
				Path:      fmt.Sprintf("/file%d.txt", index+1),
				Content:   fmt.Sprintf("content%d", index+1),
				SessionID: SessionID("session1"),
			})
			errors[index] = err
		}(i)
	}

	wg.Wait()

	// Verify all requests completed successfully
	for i, err := range errors {
		if err != nil {
			t.Errorf("Request %d failed: %v", i, err)
		}
	}

	finalCount := atomic.LoadInt64(&requestCount)
	if finalCount != 3 {
		t.Errorf("Expected 3 requests, got %d", finalCount)
	}
}

func TestMessageOrdering(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	messageLog := make([]string, 0)
	var logMutex sync.Mutex

	addToLog := func(msg string) {
		logMutex.Lock()
		defer logMutex.Unlock()
		messageLog = append(messageLog, msg)
	}

	// Create client
	testClient := &TestClient{
		writeTextFileHandler: func(params *WriteTextFileRequest) (*WriteTextFileResponse, error) {
			addToLog(fmt.Sprintf("writeTextFile called: %s", params.Path))
			return &WriteTextFileResponse{}, nil
		},
		readTextFileHandler: func(params *ReadTextFileRequest) (*ReadTextFileResponse, error) {
			addToLog(fmt.Sprintf("readTextFile called: %s", params.Path))
			return &ReadTextFileResponse{Content: "test content"}, nil
		},
		requestPermissionHandler: func(params *RequestPermissionRequest) (*RequestPermissionResponse, error) {
			addToLog(fmt.Sprintf("requestPermission called: %s", params.ToolCall.Title))
			return &RequestPermissionResponse{
				Outcome: NewRequestPermissionOutcomeSelected(PermissionOptionID("allow")),
			}, nil
		},
		sessionUpdateHandler: func(params *SessionNotification) error {
			addToLog("sessionUpdate called")
			return nil
		},
	}

	// Create agent
	testAgent := &TestAgent{
		initializeHandler: func(*InitializeRequest) (*InitializeResponse, error) {
			return &InitializeResponse{
				ProtocolVersion:   ProtocolVersion(1),
				AgentCapabilities: &AgentCapabilities{LoadSession: false},
				AuthMethods:       []AuthMethod{},
			}, nil
		},
		newSessionHandler: func(request *NewSessionRequest) (*NewSessionResponse, error) {
			addToLog(fmt.Sprintf("newSession called: %s", request.Cwd))
			return &NewSessionResponse{SessionID: SessionID("test-session")}, nil
		},
		loadSessionHandler: func(params *LoadSessionRequest) (*LoadSessionResponse, error) {
			addToLog(fmt.Sprintf("loadSession called: %s", params.SessionID))
			return &LoadSessionResponse{}, nil
		},
		authenticateHandler: func(params *AuthenticateRequest) (*AuthenticateResponse, error) {
			addToLog(fmt.Sprintf("authenticate called: %s", params.MethodID))
			return &AuthenticateResponse{}, nil
		},
		promptHandler: func(params *PromptRequest) (*PromptResponse, error) {
			addToLog(fmt.Sprintf("prompt called: %s", params.SessionID))
			return &PromptResponse{StopReason: StopReasonEndTurn}, nil
		},
		cancelHandler: func(params *CancelNotification) error {
			addToLog(fmt.Sprintf("cancelled called: %s", params.SessionID))
			return nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Send requests in specific order
	_, err := agentConnection.NewSession(ctx, &NewSessionRequest{
		Cwd:        "/test",
		MCPServers: []MCPServer{},
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	_, err = clientConnection.Client().WriteTextFile(ctx, &WriteTextFileRequest{
		Path:      "/test.txt",
		Content:   "test",
		SessionID: SessionID("test-session"),
	})
	if err != nil {
		t.Fatalf("WriteTextFile failed: %v", err)
	}

	_, err = clientConnection.Client().ReadTextFile(ctx, &ReadTextFileRequest{
		Path:      "/test.txt",
		SessionID: SessionID("test-session"),
	})
	if err != nil {
		t.Fatalf("ReadTextFile failed: %v", err)
	}

	_, err = clientConnection.Client().RequestPermission(ctx, &RequestPermissionRequest{
		SessionID: SessionID("test-session"),
		ToolCall: ToolCallUpdate{
			ToolCallID: ToolCallID("tool-123"),
			Title:      "Execute command",
			Kind:       new(ToolKindExecute),
			Status:     new(ToolCallStatusPending),
			Locations: []ToolCallLocation{
				{Path: "/usr/bin/ls"},
			},
		},
		Options: []PermissionOption{
			{
				Kind:     PermissionOptionKindAllowOnce,
				Name:     "Allow",
				OptionID: PermissionOptionID("allow"),
			},
			{
				Kind:     PermissionOptionKindRejectOnce,
				Name:     "Reject",
				OptionID: PermissionOptionID("reject"),
			},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission failed: %v", err)
	}

	// Give time for all operations to complete
	time.Sleep(200 * time.Millisecond)

	// Verify order
	expected := []string{
		"newSession called: /test",
		"writeTextFile called: /test.txt",
		"readTextFile called: /test.txt",
		"requestPermission called: Execute command",
	}

	logMutex.Lock()
	defer logMutex.Unlock()

	if len(messageLog) != len(expected) {
		t.Fatalf("Expected %d messages, got %d: %v", len(expected), len(messageLog), messageLog)
	}

	for i, expectedMsg := range expected {
		if i >= len(messageLog) || messageLog[i] != expectedMsg {
			t.Errorf("Message %d: expected '%s', got '%s'", i, expectedMsg, messageLog[i])
		}
	}
}

func TestNotifications(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	notificationLog := make([]string, 0)
	var logMutex sync.Mutex

	addToLog := func(msg string) {
		logMutex.Lock()
		defer logMutex.Unlock()
		notificationLog = append(notificationLog, msg)
	}

	// Create client
	testClient := &TestClient{
		sessionUpdateHandler: func(notification *SessionNotification) error {
			if chunk, ok := notification.Update.AsAgentMessageChunk(); ok {
				if textBlock, ok := chunk.Content.AsText(); ok {
					addToLog(fmt.Sprintf("agent message: %s", textBlock.Text))
				}
			}
			return nil
		},
	}

	// Create agent
	testAgent := &TestAgent{
		cancelHandler: func(params *CancelNotification) error {
			addToLog(fmt.Sprintf("cancelled: %s", params.SessionID))
			return nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(testClient, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Send notifications
	err := clientConnection.Client().SessionUpdate(ctx, &SessionNotification{
		SessionID: SessionID("test-session"),
		Update: NewSessionUpdateAgentMessageChunk(
			NewContentBlockText("Hello from agent"), "",
		),
	})
	if err != nil {
		t.Fatalf("SessionUpdate failed: %v", err)
	}

	// Cancel: client -> agent
	err = agentConnection.Cancel(ctx, &CancelNotification{
		SessionID: SessionID("test-session"),
	})
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Wait for async handlers
	time.Sleep(200 * time.Millisecond)

	// Verify notifications were received
	logMutex.Lock()
	defer logMutex.Unlock()

	found1 := false
	found2 := false
	for _, msg := range notificationLog {
		if msg == "agent message: Hello from agent" {
			found1 = true
		}
		if msg == "cancelled: test-session" {
			found2 = true
		}
	}

	if !found1 {
		t.Errorf("Expected 'agent message: Hello from agent' in notifications: %v", notificationLog)
	}
	if !found2 {
		t.Errorf("Expected 'cancelled: test-session' in notifications: %v", notificationLog)
	}
}

func TestInitializeMethod(t *testing.T) {
	conn := NewTestConnection()
	defer conn.Close()

	// Create agent
	testAgent := &TestAgent{
		initializeHandler: func(params *InitializeRequest) (*InitializeResponse, error) {
			return &InitializeResponse{
				ProtocolVersion: params.ProtocolVersion,
				AgentCapabilities: &AgentCapabilities{
					LoadSession: true,
					MCPCapabilities: &MCPCapabilities{
						HTTP: false,
						SSE:  true,
					},
					PromptCapabilities: &PromptCapabilities{
						Audio:           false,
						EmbeddedContext: true,
						Image:           true,
					},
				},
				AuthMethods: []AuthMethod{
					{
						ID:          "oauth",
						Name:        "OAuth",
						Description: "Authenticate with OAuth",
					},
				},
			}, nil
		},
	}

	// Set up connections
	agentConnection := NewClientSideConnection(&TestClient{}, conn.agentWriter, conn.agentToClient)
	clientConnection := NewAgentSideConnection(testAgent, conn.clientToAgent, conn.clientWriter)

	ctx := context.Background()

	// Start the connections
	go func() {
		_ = agentConnection.Start(ctx)
	}()
	go func() {
		_ = clientConnection.Start(ctx)
	}()

	// Give connections time to start
	time.Sleep(100 * time.Millisecond)

	// Test initialize request
	response, err := agentConnection.Initialize(ctx, &InitializeRequest{
		ProtocolVersion: ProtocolVersion(CurrentProtocolVersion),
		ClientCapabilities: &ClientCapabilities{
			FS: &FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
		},
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if response.ProtocolVersion != ProtocolVersion(CurrentProtocolVersion) {
		t.Errorf("Expected protocol version %d, got %d", CurrentProtocolVersion, response.ProtocolVersion)
	}

	if response.AgentCapabilities == nil {
		t.Fatal("Expected agent capabilities, got nil")
	}

	if !response.AgentCapabilities.LoadSession {
		t.Error("Expected LoadSession to be true")
	}

	if len(response.AuthMethods) != 1 {
		t.Errorf("Expected 1 auth method, got %d", len(response.AuthMethods))
	} else {
		authMethod := response.AuthMethods[0]
		if authMethod.ID != "oauth" {
			t.Errorf("Expected auth method ID 'oauth', got '%s'", authMethod.ID)
		}
		if authMethod.Name != "OAuth" {
			t.Errorf("Expected auth method name 'OAuth', got '%s'", authMethod.Name)
		}
	}
}

func TestRequestError(t *testing.T) {
	// Test RequestError type
	err := ErrMethodNotFound("test/method")
	if err.Code != ErrorCodeMethodNotFound {
		t.Errorf("Expected code %d, got %d", ErrorCodeMethodNotFound, err.Code)
	}
	if !strings.Contains(err.Error(), "test/method") {
		t.Errorf("Expected error message to contain 'test/method', got: %s", err.Error())
	}

	err2 := ErrInvalidParams("bad field", "custom message")
	if err2.Code != ErrorCodeInvalidParams {
		t.Errorf("Expected code %d, got %d", ErrorCodeInvalidParams, err2.Code)
	}
	if err2.Msg != "custom message" {
		t.Errorf("Expected message 'custom message', got '%s'", err2.Msg)
	}
	if err2.Details != "bad field" {
		t.Errorf("Expected details 'bad field', got '%v'", err2.Details)
	}
}

func TestNewHelperConstructors(t *testing.T) {
	// Test new content block constructors
	t.Run("ContentBlockAudio", func(t *testing.T) {
		block := NewContentBlockAudio("base64data", "audio/mp3")
		audio, ok := block.AsAudio()
		if !ok {
			t.Fatal("Expected audio content block")
		}
		if audio.Data != "base64data" {
			t.Errorf("Expected data 'base64data', got '%s'", audio.Data)
		}
		if audio.MimeType != "audio/mp3" {
			t.Errorf("Expected mimeType 'audio/mp3', got '%s'", audio.MimeType)
		}
	})

	// Test new session update constructors
	t.Run("SessionUpdateUserMessageChunk", func(t *testing.T) {
		update := NewSessionUpdateUserMessageChunk(NewContentBlockText("user input"), "")
		chunk, ok := update.AsUserMessageChunk()
		if !ok {
			t.Fatal("Expected user message chunk")
		}
		text, ok := chunk.Content.AsText()
		if !ok {
			t.Fatal("Expected text content")
		}
		if text.Text != "user input" {
			t.Errorf("Expected 'user input', got '%s'", text.Text)
		}
	})

	t.Run("SessionUpdateCurrentModeUpdate", func(t *testing.T) {
		update := NewSessionUpdateCurrentModeUpdate(SessionModeID("code"))
		mode, ok := update.AsCurrentModeUpdate()
		if !ok {
			t.Fatal("Expected current mode update")
		}
		if mode.CurrentModeID != SessionModeID("code") {
			t.Errorf("Expected mode 'code', got '%s'", mode.CurrentModeID)
		}
	})

	// Test MCP server constructors
	t.Run("MCPServerStdio", func(t *testing.T) {
		server := NewMCPServerStdio("test-server", "/usr/bin/test", []string{"--arg1"}, []EnvVariable{{Name: "KEY", Value: "val"}})
		stdio, ok := server.AsStdio()
		if !ok {
			t.Fatal("Expected stdio MCP server")
		}
		if stdio.Name != "test-server" {
			t.Errorf("Expected name 'test-server', got '%s'", stdio.Name)
		}
		if stdio.Command != "/usr/bin/test" {
			t.Errorf("Expected command '/usr/bin/test', got '%s'", stdio.Command)
		}
	})

	// Test tool call content terminal
	t.Run("ToolCallContentTerminal", func(t *testing.T) {
		content := NewToolCallContentTerminal("term-123")
		terminal, ok := content.AsTerminal()
		if !ok {
			t.Fatal("Expected terminal tool call content")
		}
		if terminal.TerminalID != "term-123" {
			t.Errorf("Expected terminal ID 'term-123', got '%s'", terminal.TerminalID)
		}
	})

	// Test new(constant) for pointer creation (Go 1.26+)
	t.Run("NewConstantPointer", func(t *testing.T) {
		kind := new(ToolKindRead)
		if *kind != ToolKindRead {
			t.Errorf("Expected ToolKindRead, got %s", *kind)
		}
		status := new(ToolCallStatusPending)
		if *status != ToolCallStatusPending {
			t.Errorf("Expected ToolCallStatusPending, got %s", *status)
		}
	})
}

// TestEOFDoesNotDropInFlightResponses verifies that when stdin closes (EOF)
// while a request handler is still running, the response is still written
// to stdout before the connection shuts down.
func TestEOFDoesNotDropInFlightResponses(t *testing.T) {
	// Set up pipes: we write JSON-RPC requests to stdinWriter,
	// the connection reads from stdinReader. Responses go to stdoutWriter,
	// we read them from stdoutReader.
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	handlerStarted := make(chan struct{})

	// Create a connection with a handler that takes some time to respond.
	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "initialize" {
			close(handlerStarted)
			// Simulate a slow handler
			time.Sleep(100 * time.Millisecond)
			return &InitializeResponse{
				ProtocolVersion:   ProtocolVersion(CurrentProtocolVersion),
				AgentCapabilities: &AgentCapabilities{LoadSession: false},
				AuthMethods:       []AuthMethod{},
			}, nil
		}
		return nil, fmt.Errorf("unknown method: %s", method)
	}

	conn := NewConnection(handler, stdinReader, stdoutWriter)

	// Start the connection in a goroutine.
	startErr := make(chan error, 1)
	go func() {
		startErr <- conn.Start(context.Background())
	}()

	// Send an initialize request, then immediately close stdin.
	reqID := int64(1)
	reqMsg := jsonRpcMessage{
		Jsonrpc: jsonrpcVersion,
		ID:      &reqID,
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":1,"clientCapabilities":{}}`),
	}
	reqData, err := json.Marshal(reqMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}
	reqData = append(reqData, '\n')

	// Write the request.
	if _, err := stdinWriter.Write(reqData); err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Wait for the handler to start processing, then close stdin (EOF).
	<-handlerStarted
	stdinWriter.Close()

	// Read the response from stdout. The bug would cause this to hang
	// or return EOF without a response.
	responseCh := make(chan json.RawMessage, 1)
	readErrCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdoutReader)
		scanner.Buffer(make([]byte, 0, initialBufSize), maxMessageSize)
		if scanner.Scan() {
			data := make([]byte, len(scanner.Bytes()))
			copy(data, scanner.Bytes())
			responseCh <- json.RawMessage(data)
		} else {
			if scanner.Err() != nil {
				readErrCh <- scanner.Err()
			} else {
				readErrCh <- fmt.Errorf("stdout closed (EOF) without response")
			}
		}
	}()

	// Wait for the response with a timeout.
	select {
	case resp := <-responseCh:
		// Verify it's a valid JSON-RPC response with our request ID.
		var msg jsonRpcMessage
		if err := json.Unmarshal(resp, &msg); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if msg.ID == nil || *msg.ID != reqID {
			t.Fatalf("Expected response ID %d, got %v", reqID, msg.ID)
		}
		if msg.Error != nil {
			t.Fatalf("Expected successful response, got error: %s", msg.Error.Message)
		}
		if msg.Result == nil {
			t.Fatal("Expected result in response, got nil")
		}
	case readErr := <-readErrCh:
		t.Fatalf("Failed to read response (in-flight response was dropped): %v", readErr)
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for response — in-flight response was likely dropped")
	}

	// Verify Start returned cleanly.
	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for Start to return")
	}

	stdoutWriter.Close()
	stdoutReader.Close()
}
