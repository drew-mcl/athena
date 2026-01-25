package runner

import (
	"context"
	"fmt"
	"os"
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
		SessionID:       spec.SessionID,
		WorkDir:         spec.WorkDir,
		Model:           spec.Model,
		PermissionMode:  spec.PermissionMode,
		AllowedTools:    spec.AllowedTools,
		SystemPrompt:    spec.SystemPrompt,
		MaxBudgetUSD:    spec.MaxBudgetUSD,
		Plan:            spec.Plan,
		LogFile:         spec.LogFile,
	}
	return newGeminiSession(ctx, runSpec, &spec)
}

func (r *GeminiRunner) Attach(ctx context.Context, pid int, opts AttachOptions) (Session, error) {
	return nil, fmt.Errorf("attach not supported for gemini runner")
}

type geminiSession struct {
	id         string
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
	if spec.SystemPrompt != "" {
		model.SystemInstruction = &genai.Content{
			Parts: []genai.Part{genai.Text(spec.SystemPrompt)},
		}
	}

	// TODO: Configure tools based on spec.AllowedTools

	cs := model.StartChat()
	// TODO: Load history if resuming (resume != nil)

	ctx, cancel := context.WithCancel(ctx)
	s := &geminiSession{
		id:        spec.SessionID,
		ctx:       ctx,
		cancel:    cancel,
		client:    client,
		model:     model,
		chat:      cs,
		events:    make(chan Event, 100),
		errors:    make(chan error, 10),
		done:      make(chan struct{}),
		input:     make(chan any),
		startTime: time.Now(),
	}

	s.wg.Add(1)
	go s.runLoop()

	if spec.Prompt != "" {
		// Queue initial prompt
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
	// Parse input message (expecting map or specific struct)
	// For now assume it's map[string]string or similar
	var content string
	
	// Handle different input types
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

	// Process response
	s.processResponse(resp)
}

func (s *geminiSession) processResponse(resp *genai.GenerateContentResponse) {
	if resp == nil {
		return
	}

	// Track usage
	var eventUsage *EventUsage
	if resp.UsageMetadata != nil {
		s.modelUsage.InputTokens += int(resp.UsageMetadata.PromptTokenCount)
		s.modelUsage.OutputTokens += int(resp.UsageMetadata.CandidatesTokenCount)
		
		eventUsage = &EventUsage{
			InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
			OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			// CacheReads: ... (TODO: Extract from proto if available)
		}
	}

	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if txt, ok := part.(genai.Text); ok {
					s.events <- Event{
						Type:      "assistant",
						Content:   string(txt),
						SessionID: s.id,
						Timestamp: time.Now(),
						Usage:     eventUsage,
					}
					// Only attach usage to the first event to avoid double counting?
					// Or assume consumer handles it.
					eventUsage = nil 
				}
				// TODO: Handle function calls (part.(genai.FunctionCall))
			}
		}
	}
}
