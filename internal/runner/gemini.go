package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GeminiRunner implements Runner using Google Gemini API.
type GeminiRunner struct{}

func (r *GeminiRunner) Provider() string {
	return ProviderGemini
}

func (r *GeminiRunner) Capabilities() Capabilities {
	return Capabilities{
		JSONInput:    true,
		JSONOutput:   true,
		SessionID:    true,
		Resume:       true,
		ForkSession:  false,
		Plan:         true,
		AllowedTools: true,
		SystemPrompt: true,
		MaxTurns:     true,
	}
}

func (r *GeminiRunner) Start(ctx context.Context, spec RunSpec) (Session, error) {
	return newGeminiSession(ctx, spec, nil)
}

func (r *GeminiRunner) Resume(ctx context.Context, spec ResumeSpec) (Session, error) {
	// Convert ResumeSpec to RunSpec
	runSpec := RunSpec{
		SessionID:      spec.SessionID,
		WorkDir:        spec.WorkDir,
		Model:          spec.Model,
		PermissionMode: spec.PermissionMode,
		AllowedTools:   spec.AllowedTools,
		SystemPrompt:   spec.SystemPrompt,
		MaxBudgetUSD:   spec.MaxBudgetUSD,
		Plan:           spec.Plan,
		LogFile:        spec.LogFile,
	}
	return newGeminiSession(ctx, runSpec, &spec)
}

func (r *GeminiRunner) Attach(ctx context.Context, pid int, opts AttachOptions) (Session, error) {
	return nil, fmt.Errorf("attach not supported for gemini runner")
}

type geminiSession struct {
	id         string
	workDir    string
	ctx        context.Context
	cancel     context.CancelFunc
	client     *genai.Client
	model      *genai.GenerativeModel
	chat       *genai.ChatSession
	events     chan Event
	errors     chan error
	done       chan struct{}
	input      chan any
	wg         sync.WaitGroup
	startTime  time.Time
	modelUsage ModelUsage
	historyDir string
}

// ModelUsage tracks token usage and cache hits
type ModelUsage struct {
	Requests     int
	InputTokens  int
	CacheReads   int
	OutputTokens int
}

func newGeminiSession(ctx context.Context, spec RunSpec, resume *ResumeSpec) (*geminiSession, error) {
	apiKey := spec.Env["GEMINI_API_KEY"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	modelName := spec.Model
	if modelName == "" {
		modelName = "gemini-2.0-flash-exp"
	}

	model := client.GenerativeModel(modelName)

	// Configure tools
	model.Tools = getTools(spec.AllowedTools)

	if spec.SystemPrompt != "" {
		model.SystemInstruction = &genai.Content{
			Parts: []genai.Part{genai.Text(spec.SystemPrompt)},
		}
	}

	cs := model.StartChat()

	// Setup history persistence directory
	home, _ := os.UserHomeDir()
	historyDir := filepath.Join(home, ".local", "share", "athena", "gemini", "sessions")
	if err := os.MkdirAll(historyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create history dir: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &geminiSession{
		id:         spec.SessionID,
		workDir:    spec.WorkDir,
		ctx:        ctx,
		cancel:     cancel,
		client:     client,
		model:      model,
		chat:       cs,
		events:     make(chan Event, 100),
		errors:     make(chan error, 10),
		done:       make(chan struct{}),
		input:      make(chan any),
		startTime:  time.Now(),
		historyDir: historyDir,
	}

	// Load history if resuming
	if resume != nil {
		if err := s.loadHistory(); err != nil {
			// Log warning but continue?
			// For now, fail to ensure we don't start with empty context unexpectedly
			// fmt.Printf("failed to load history: %v\n", err)
		}
	}

	s.wg.Add(1)
	go s.runLoop()

	if spec.Prompt != "" && resume == nil {
		// Queue initial prompt only if not resuming (or if explicitly requested?)
		// Usually ResumeSpec doesn't have a prompt, but RunSpec does.
		// If we are starting fresh, send prompt.
		go func() {
			select {
			case s.input <- map[string]string{"type": "user", "content": spec.Prompt}:
			case <-s.ctx.Done():
			}
		}()
	}

	return s, nil
}

func (s *geminiSession) ID() string {
	return s.id
}

func (s *geminiSession) PID() int {
	return 0 // No process ID for API
}

func (s *geminiSession) IsRunning() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *geminiSession) ExitCode() int {
	return 0
}

func (s *geminiSession) SendJSON(msg any) error {
	select {
	case s.input <- msg:
		return nil
	case <-s.done:
		return fmt.Errorf("session closed")
	}
}

func (s *geminiSession) Events() <-chan Event {
	return s.events
}

func (s *geminiSession) Errors() <-chan error {
	return s.errors
}

func (s *geminiSession) Done() <-chan struct{} {
	return s.done
}

func (s *geminiSession) Stop() error {
	s.cancel()
	return nil
}

func (s *geminiSession) runLoop() {
	defer s.wg.Done()
	defer close(s.events)
	defer close(s.done)
	defer s.client.Close()

	for {
		select {
		case <-s.ctx.Done():
			return
		case msg := <-s.input:
			s.handleInput(msg)
		}
	}
}

func (s *geminiSession) handleInput(msg any) {
	// Parse input message
	var content string

	switch v := msg.(type) {
	case string:
		content = v
	case map[string]string:
		content = v["content"]
	case map[string]interface{}:
		if c, ok := v["content"].(string); ok {
			content = c
		}
	}

	if content == "" {
		return
	}

	s.modelUsage.Requests++

	// Send to Gemini
	resp, err := s.chat.SendMessage(s.ctx, genai.Text(content))
	if err != nil {
		s.errors <- err
		return
	}

	// Process response (and handle tool calls recursively)
	s.processResponse(resp)

	// Save history after turn
	_ = s.saveHistory()
}

func (s *geminiSession) processResponse(resp *genai.GenerateContentResponse) {
	if resp == nil {
		return
	}

	eventUsage := s.updateUsage(resp)

	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			eventUsage = s.processCandidateParts(cand.Content.Parts, eventUsage)
		}
	}
}

func (s *geminiSession) updateUsage(resp *genai.GenerateContentResponse) *EventUsage {
	if resp.UsageMetadata == nil {
		return nil
	}
	s.modelUsage.InputTokens += int(resp.UsageMetadata.PromptTokenCount)
	s.modelUsage.OutputTokens += int(resp.UsageMetadata.CandidatesTokenCount)

	return &EventUsage{
		InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
		OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
	}
}

func (s *geminiSession) processCandidateParts(parts []genai.Part, eventUsage *EventUsage) *EventUsage {
	for _, part := range parts {
		switch p := part.(type) {
		case genai.Text:
			eventUsage = s.emitAssistantText(string(p), eventUsage)
		case genai.FunctionCall:
			if err := s.handleFunctionCall(p); err != nil {
				s.errors <- err
				return nil
			}
		}
	}
	return eventUsage
}

func (s *geminiSession) emitAssistantText(text string, eventUsage *EventUsage) *EventUsage {
	s.events <- Event{
		Type:      "assistant",
		Content:   text,
		SessionID: s.id,
		Timestamp: time.Now(),
		Usage:     eventUsage,
	}
	return nil
}

func (s *geminiSession) handleFunctionCall(call genai.FunctionCall) error {
	s.emitToolUse(call)
	result, err := s.executeTool(call.Name, call.Args)
	s.emitToolResult(call.Name, result, err)

	nextResp, err := s.sendToolResponse(call.Name, result)
	if err != nil {
		return fmt.Errorf("failed to send tool response: %w", err)
	}
	s.processResponse(nextResp)
	return nil
}

func (s *geminiSession) emitToolUse(call genai.FunctionCall) {
	argsJSON, _ := json.Marshal(call.Args)
	s.events <- Event{
		Type:      "tool_use",
		Name:      call.Name,
		Input:     argsJSON,
		SessionID: s.id,
		Timestamp: time.Now(),
	}
}

func (s *geminiSession) emitToolResult(name string, result any, err error) {
	resultStr := fmt.Sprintf("%v", result)
	if err != nil {
		resultStr = fmt.Sprintf("Error: %v", err)
	}

	s.events <- Event{
		Type:      "tool_result",
		Name:      name,
		Content:   resultStr,
		SessionID: s.id,
		Timestamp: time.Now(),
	}
}

func (s *geminiSession) sendToolResponse(name string, result any) (*genai.GenerateContentResponse, error) {
	return s.chat.SendMessage(s.ctx, genai.FunctionResponse{
		Name: name,
		Response: map[string]any{
			"result": result,
		},
	})
}

// Tool Implementation

func getTools(allowed []string) []*genai.Tool {
	// If no tools allowed, return nil
	if len(allowed) == 0 {
		return nil
	}

	// Basic file system tools
	return []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "read_file",
					Description: "Read the contents of a file",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {Type: genai.TypeString, Description: "Path to the file"},
						},
						Required: []string{"path"},
					},
				},
				{
					Name:        "write_file",
					Description: "Write content to a file",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path":    {Type: genai.TypeString, Description: "Path to the file"},
							"content": {Type: genai.TypeString, Description: "Content to write"},
						},
						Required: []string{"path", "content"},
					},
				},
				{
					Name:        "list_files",
					Description: "List files in a directory",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"path": {Type: genai.TypeString, Description: "Path to the directory"},
						},
						Required: []string{"path"},
					},
				},
				{
					Name:        "run_command",
					Description: "Run a shell command (limited)",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"command": {Type: genai.TypeString, Description: "Command to run (ls, grep, cat)"},
							"args":    {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
						},
						Required: []string{"command"},
					},
				},
			},
		},
	}
}

func (s *geminiSession) executeTool(name string, args map[string]any) (any, error) {
	switch name {
	case "read_file":
		return s.toolReadFile(args)
	case "write_file":
		return s.toolWriteFile(args)
	case "list_files":
		return s.toolListFiles(args)
	case "run_command":
		return s.toolRunCommand(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *geminiSession) toolReadFile(args map[string]any) (any, error) {
	path, err := requiredStringArg(args, "path")
	if err != nil {
		return nil, err
	}
	safe, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(safe)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (s *geminiSession) toolWriteFile(args map[string]any) (any, error) {
	path, err := requiredStringArg(args, "path")
	if err != nil {
		return nil, err
	}
	content, err := requiredStringArg(args, "content")
	if err != nil {
		return nil, err
	}
	safe, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(safe, []byte(content), 0644); err != nil {
		return nil, err
	}
	return "success", nil
}

func (s *geminiSession) toolListFiles(args map[string]any) (any, error) {
	path := optionalStringArg(args, "path", ".")
	safe, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(safe)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		prefix := ""
		if e.IsDir() {
			prefix = "/"
		}
		files = append(files, e.Name()+prefix)
	}
	return strings.Join(files, "\n"), nil
}

var allowedCommands = map[string]bool{"ls": true, "grep": true, "cat": true, "find": true}

func (s *geminiSession) toolRunCommand(args map[string]any) (any, error) {
	cmd, err := requiredStringArg(args, "command")
	if err != nil {
		return nil, err
	}
	cmdArgs := parseArgs(args["args"])
	if !allowedCommands[cmd] {
		return nil, fmt.Errorf("command not allowed: %s", cmd)
	}

	c := exec.CommandContext(s.ctx, cmd, cmdArgs...)
	c.Dir = s.workDir
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error: %v\nOutput: %s", err, out), nil
	}
	return string(out), nil
}

func requiredStringArg(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("invalid %s argument", key)
	}
	return value, nil
}

func optionalStringArg(args map[string]any, key, fallback string) string {
	if value, ok := args[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func parseArgs(raw any) []string {
	rawArgs, ok := raw.([]any)
	if !ok {
		return nil
	}
	args := make([]string, 0, len(rawArgs))
	for _, a := range rawArgs {
		args = append(args, fmt.Sprint(a))
	}
	return args
}

func (s *geminiSession) safePath(p string) (string, error) {
	absWork, err := filepath.Abs(s.workDir)
	if err != nil {
		return "", err
	}

	target := filepath.Join(absWork, p)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(absTarget, absWork) {
		return "", fmt.Errorf("path access denied: %s is outside workdir %s", p, s.workDir)
	}
	return absTarget, nil
}

// Persistence

type serializableContent struct {
	Role  string             `json:"role"`
	Parts []serializablePart `json:"parts"`
}

type serializablePart struct {
	Type             string                  `json:"type"` // "text", "blob", "function_call", "function_response"
	Text             string                  `json:"text,omitempty"`
	Blob             *genai.Blob             `json:"blob,omitempty"`
	FunctionCall     *genai.FunctionCall     `json:"function_call,omitempty"`
	FunctionResponse *genai.FunctionResponse `json:"function_response,omitempty"`
}

func (s *geminiSession) saveHistory() error {
	if s.historyDir == "" {
		return nil
	}

	var stored []serializableContent
	for _, c := range s.chat.History {
		sc := serializableContent{Role: c.Role}
		for _, p := range c.Parts {
			sp := serializablePart{}
			switch v := p.(type) {
			case genai.Text:
				sp.Type = "text"
				sp.Text = string(v)
			case genai.Blob:
				sp.Type = "blob"
				sp.Blob = &v
			case genai.FunctionCall:
				sp.Type = "function_call"
				sp.FunctionCall = &v
			case genai.FunctionResponse:
				sp.Type = "function_response"
				sp.FunctionResponse = &v
			default:
				// Skip unsupported parts
				continue
			}
			sc.Parts = append(sc.Parts, sp)
		}
		stored = append(stored, sc)
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	filename := filepath.Join(s.historyDir, s.id+".json")
	return os.WriteFile(filename, data, 0600)
}

func (s *geminiSession) loadHistory() error {
	filename := filepath.Join(s.historyDir, s.id+".json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	history, err := decodeHistory(data)
	if err != nil {
		return err
	}
	s.chat.History = history
	return nil
}

func decodeHistory(data []byte) ([]*genai.Content, error) {
	var stored []serializableContent
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("failed to unmarshal history: %w", err)
	}
	return buildHistory(stored), nil
}

func buildHistory(stored []serializableContent) []*genai.Content {
	history := make([]*genai.Content, 0, len(stored))
	for _, sc := range stored {
		history = append(history, buildContent(sc))
	}
	return history
}

func buildContent(sc serializableContent) *genai.Content {
	c := &genai.Content{Role: sc.Role}
	for _, sp := range sc.Parts {
		if part, ok := buildPart(sp); ok {
			c.Parts = append(c.Parts, part)
		}
	}
	return c
}

func buildPart(sp serializablePart) (genai.Part, bool) {
	switch sp.Type {
	case "text":
		return genai.Text(sp.Text), true
	case "blob":
		if sp.Blob != nil {
			return *sp.Blob, true
		}
	case "function_call":
		if sp.FunctionCall != nil {
			return *sp.FunctionCall, true
		}
	case "function_response":
		if sp.FunctionResponse != nil {
			return *sp.FunctionResponse, true
		}
	}
	return nil, false
}
