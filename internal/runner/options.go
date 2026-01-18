package runner

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Common Types (shared semantics across providers)
// =============================================================================

// Model represents a model identifier.
// Can be an alias (sonnet, opus, haiku, o3) or full name.
type Model string

// Common model aliases
const (
	ModelSonnet Model = "sonnet"
	ModelOpus   Model = "opus"
	ModelHaiku  Model = "haiku"
	ModelO3     Model = "o3"
	ModelO4Mini Model = "o4-mini"
)

// OutputFormat specifies how output should be formatted.
type OutputFormat string

const (
	OutputFormatText       OutputFormat = "text"
	OutputFormatJSON       OutputFormat = "json"
	OutputFormatStreamJSON OutputFormat = "stream-json"
)

// InputFormat specifies how input should be formatted.
type InputFormat string

const (
	InputFormatText       InputFormat = "text"
	InputFormatStreamJSON InputFormat = "stream-json"
)

// =============================================================================
// Claude Code Options
// =============================================================================

// ClaudePermissionMode controls Claude's permission behavior.
type ClaudePermissionMode string

const (
	ClaudePermissionDefault           ClaudePermissionMode = "default"
	ClaudePermissionPlan              ClaudePermissionMode = "plan"
	ClaudePermissionAcceptEdits       ClaudePermissionMode = "acceptEdits"
	ClaudePermissionBypassPermissions ClaudePermissionMode = "bypassPermissions"
	ClaudePermissionDelegate          ClaudePermissionMode = "delegate"
	ClaudePermissionDontAsk           ClaudePermissionMode = "dontAsk"
)

// ClaudeOptions contains all Claude Code CLI options.
type ClaudeOptions struct {
	// === Core ===
	Prompt string // Positional argument
	Model  Model  // --model

	// === Session Management ===
	SessionID   string // --session-id (UUID)
	Resume      string // --resume [session_id or empty for picker]
	Continue    bool   // --continue (most recent in cwd)
	ForkSession bool   // --fork-session

	// === Working Directory & Access ===
	// Note: WorkDir is set via process cwd, not a flag
	AddDirs []string // --add-dir

	// === Permissions ===
	PermissionMode                  ClaudePermissionMode // --permission-mode
	AllowDangerouslySkipPermissions bool                 // --allow-dangerously-skip-permissions
	DangerouslySkipPermissions      bool                 // --dangerously-skip-permissions

	// === Tools ===
	AllowedTools    []string // --allowedTools, --allowed-tools
	DisallowedTools []string // --disallowedTools, --disallowed-tools
	Tools           []string // --tools (built-in set control)

	// === System Prompts ===
	SystemPrompt       string // --system-prompt (replaces default)
	AppendSystemPrompt string // --append-system-prompt

	// === Agent Configuration ===
	Agent  string          // --agent (agent name)
	Agents json.RawMessage // --agents (JSON object)

	// === Output Control ===
	Print                  bool         // --print (-p)
	OutputFormat           OutputFormat // --output-format
	InputFormat            InputFormat  // --input-format
	IncludePartialMessages bool         // --include-partial-messages
	ReplayUserMessages     bool         // --replay-user-messages
	Verbose                bool         // --verbose

	// === Structured Output ===
	JSONSchema json.RawMessage // --json-schema

	// === Limits ===
	MaxBudgetUSD float64 // --max-budget-usd

	// === MCP Configuration ===
	MCPConfig       []string // --mcp-config
	StrictMCPConfig bool     // --strict-mcp-config
	MCPDebug        bool     // --mcp-debug (deprecated)

	// === Plugins ===
	PluginDirs []string // --plugin-dir

	// === Settings ===
	Settings       string   // --settings (file or JSON)
	SettingSources []string // --setting-sources

	// === IDE/Chrome Integration ===
	IDE      bool // --ide
	Chrome   bool // --chrome
	NoChrome bool // --no-chrome

	// === Debug ===
	Debug string // --debug [filter]

	// === File Resources ===
	Files []string // --file (file_id:path format)

	// === Model Fallback ===
	FallbackModel Model // --fallback-model

	// === Session Persistence ===
	NoSessionPersistence bool // --no-session-persistence

	// === Slash Commands ===
	DisableSlashCommands bool // --disable-slash-commands

	// === Beta Features ===
	Betas []string // --betas
}

// =============================================================================
// Codex Options
// =============================================================================

// CodexSandboxMode controls Codex's sandbox behavior.
type CodexSandboxMode string

const (
	CodexSandboxReadOnly         CodexSandboxMode = "read-only"
	CodexSandboxWorkspaceWrite   CodexSandboxMode = "workspace-write"
	CodexSandboxDangerFullAccess CodexSandboxMode = "danger-full-access"
)

// CodexApprovalPolicy controls when Codex asks for approval.
type CodexApprovalPolicy string

const (
	CodexApprovalUntrusted CodexApprovalPolicy = "untrusted"
	CodexApprovalOnFailure CodexApprovalPolicy = "on-failure"
	CodexApprovalOnRequest CodexApprovalPolicy = "on-request"
	CodexApprovalNever     CodexApprovalPolicy = "never"
)

// CodexColorMode controls output coloring.
type CodexColorMode string

const (
	CodexColorAlways CodexColorMode = "always"
	CodexColorNever  CodexColorMode = "never"
	CodexColorAuto   CodexColorMode = "auto"
)

// CodexLocalProvider specifies the local LLM provider.
type CodexLocalProvider string

const (
	CodexLocalLMStudio    CodexLocalProvider = "lmstudio"
	CodexLocalOllama      CodexLocalProvider = "ollama"
	CodexLocalOllamaChat  CodexLocalProvider = "ollama-chat"
)

// CodexOptions contains all Codex CLI options.
type CodexOptions struct {
	// === Core ===
	Prompt string // Positional argument (or stdin with "-")
	Model  Model  // -m, --model

	// === Working Directory ===
	WorkDir string   // -C, --cd
	AddDirs []string // --add-dir

	// === Sandbox & Approvals ===
	SandboxMode                          CodexSandboxMode    // -s, --sandbox
	ApprovalPolicy                       CodexApprovalPolicy // -a, --ask-for-approval
	FullAuto                             bool                // --full-auto
	DangerouslyBypassApprovalsAndSandbox bool                // --dangerously-bypass-approvals-and-sandbox

	// === Configuration ===
	Config  map[string]string // -c, --config (key=value pairs)
	Profile string            // -p, --profile
	Enable  []string          // --enable (features)
	Disable []string          // --disable (features)

	// === Local/OSS Models ===
	OSS           bool               // --oss
	LocalProvider CodexLocalProvider // --local-provider

	// === Images ===
	Images []string // -i, --image

	// === Output ===
	JSON              bool   // --json (JSONL output)
	OutputLastMessage string // -o, --output-last-message
	Color             CodexColorMode // --color

	// === Structured Output ===
	OutputSchema string // --output-schema (path to JSON schema file)

	// === Git ===
	SkipGitRepoCheck bool // --skip-git-repo-check

	// === TUI ===
	NoAltScreen bool // --no-alt-screen

	// === Search ===
	Search bool // --search (enable web search)
}

// =============================================================================
// Unified RunSpec with Provider-Specific Extensions
// =============================================================================

// ExtendedRunSpec extends RunSpec with full provider options.
type ExtendedRunSpec struct {
	RunSpec

	// Provider-specific options (only one should be set)
	Claude *ClaudeOptions
	Codex  *CodexOptions
}

// ExtendedResumeSpec extends ResumeSpec with full provider options.
type ExtendedResumeSpec struct {
	ResumeSpec

	// Provider-specific options (only one should be set)
	Claude *ClaudeOptions
	Codex  *CodexOptions
}

// =============================================================================
// Option Builders (fluent API)
// =============================================================================

// ClaudeOptionsBuilder provides a fluent API for building ClaudeOptions.
type ClaudeOptionsBuilder struct {
	opts ClaudeOptions
}

// NewClaudeOptions starts building ClaudeOptions.
func NewClaudeOptions() *ClaudeOptionsBuilder {
	return &ClaudeOptionsBuilder{}
}

func (b *ClaudeOptionsBuilder) WithPrompt(p string) *ClaudeOptionsBuilder {
	b.opts.Prompt = p
	return b
}

func (b *ClaudeOptionsBuilder) WithModel(m Model) *ClaudeOptionsBuilder {
	b.opts.Model = m
	return b
}

func (b *ClaudeOptionsBuilder) WithSessionID(id string) *ClaudeOptionsBuilder {
	b.opts.SessionID = id
	return b
}

func (b *ClaudeOptionsBuilder) WithPermissionMode(m ClaudePermissionMode) *ClaudeOptionsBuilder {
	b.opts.PermissionMode = m
	return b
}

func (b *ClaudeOptionsBuilder) WithAllowedTools(tools ...string) *ClaudeOptionsBuilder {
	b.opts.AllowedTools = tools
	return b
}

func (b *ClaudeOptionsBuilder) WithSystemPrompt(p string) *ClaudeOptionsBuilder {
	b.opts.SystemPrompt = p
	return b
}

func (b *ClaudeOptionsBuilder) WithAppendSystemPrompt(p string) *ClaudeOptionsBuilder {
	b.opts.AppendSystemPrompt = p
	return b
}


func (b *ClaudeOptionsBuilder) WithPrint() *ClaudeOptionsBuilder {
	b.opts.Print = true
	return b
}

func (b *ClaudeOptionsBuilder) WithStreamJSON() *ClaudeOptionsBuilder {
	b.opts.OutputFormat = OutputFormatStreamJSON
	b.opts.InputFormat = InputFormatStreamJSON
	return b
}

func (b *ClaudeOptionsBuilder) WithVerbose() *ClaudeOptionsBuilder {
	b.opts.Verbose = true
	return b
}

func (b *ClaudeOptionsBuilder) Build() ClaudeOptions {
	return b.opts
}

// CodexOptionsBuilder provides a fluent API for building CodexOptions.
type CodexOptionsBuilder struct {
	opts CodexOptions
}

// NewCodexOptions starts building CodexOptions.
func NewCodexOptions() *CodexOptionsBuilder {
	return &CodexOptionsBuilder{}
}

func (b *CodexOptionsBuilder) WithPrompt(p string) *CodexOptionsBuilder {
	b.opts.Prompt = p
	return b
}

func (b *CodexOptionsBuilder) WithModel(m Model) *CodexOptionsBuilder {
	b.opts.Model = m
	return b
}

func (b *CodexOptionsBuilder) WithWorkDir(d string) *CodexOptionsBuilder {
	b.opts.WorkDir = d
	return b
}

func (b *CodexOptionsBuilder) WithSandbox(m CodexSandboxMode) *CodexOptionsBuilder {
	b.opts.SandboxMode = m
	return b
}

func (b *CodexOptionsBuilder) WithApproval(p CodexApprovalPolicy) *CodexOptionsBuilder {
	b.opts.ApprovalPolicy = p
	return b
}

func (b *CodexOptionsBuilder) WithFullAuto() *CodexOptionsBuilder {
	b.opts.FullAuto = true
	return b
}

func (b *CodexOptionsBuilder) WithJSON() *CodexOptionsBuilder {
	b.opts.JSON = true
	return b
}

func (b *CodexOptionsBuilder) Build() CodexOptions {
	return b.opts
}

// =============================================================================
// Conversion to CLI Args
// =============================================================================

// Args converts ClaudeOptions to CLI arguments.
func (o *ClaudeOptions) Args() []string {
	var args []string

	// Output format (must come first for pipe mode)
	if o.Print {
		args = append(args, "--print")
	}
	if o.Verbose {
		args = append(args, "--verbose")
	}
	if o.OutputFormat != "" {
		args = append(args, "--output-format", string(o.OutputFormat))
	}
	if o.InputFormat != "" {
		args = append(args, "--input-format", string(o.InputFormat))
	}

	// Session
	if o.SessionID != "" {
		args = append(args, "--session-id", o.SessionID)
	}
	if o.Resume != "" {
		args = append(args, "--resume", o.Resume)
	} else if o.Resume == "" && o.Continue {
		args = append(args, "--continue")
	}
	if o.ForkSession {
		args = append(args, "--fork-session")
	}

	// Model
	if o.Model != "" {
		args = append(args, "--model", string(o.Model))
	}
	if o.FallbackModel != "" {
		args = append(args, "--fallback-model", string(o.FallbackModel))
	}

	// Permissions
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", string(o.PermissionMode))
	}
	if o.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if o.AllowDangerouslySkipPermissions {
		args = append(args, "--allow-dangerously-skip-permissions")
	}

	// Tools
	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, o.AllowedTools...)
	}
	if len(o.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, o.DisallowedTools...)
	}
	if len(o.Tools) > 0 {
		args = append(args, "--tools")
		args = append(args, o.Tools...)
	}

	// System prompts
	if o.SystemPrompt != "" {
		args = append(args, "--system-prompt", o.SystemPrompt)
	}
	if o.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", o.AppendSystemPrompt)
	}

	// Agent
	if o.Agent != "" {
		args = append(args, "--agent", o.Agent)
	}
	if len(o.Agents) > 0 {
		args = append(args, "--agents", string(o.Agents))
	}

	// Limits
	if o.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", ftoa(o.MaxBudgetUSD))
	}

	// Structured output
	if len(o.JSONSchema) > 0 {
		args = append(args, "--json-schema", string(o.JSONSchema))
	}

	// Partial messages
	if o.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	if o.ReplayUserMessages {
		args = append(args, "--replay-user-messages")
	}

	// MCP
	for _, cfg := range o.MCPConfig {
		args = append(args, "--mcp-config", cfg)
	}
	if o.StrictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}

	// Plugins
	for _, dir := range o.PluginDirs {
		args = append(args, "--plugin-dir", dir)
	}

	// Settings
	if o.Settings != "" {
		args = append(args, "--settings", o.Settings)
	}
	if len(o.SettingSources) > 0 {
		args = append(args, "--setting-sources", joinComma(o.SettingSources))
	}

	// Directories
	for _, dir := range o.AddDirs {
		args = append(args, "--add-dir", dir)
	}

	// Files
	for _, f := range o.Files {
		args = append(args, "--file", f)
	}

	// IDE/Chrome
	if o.IDE {
		args = append(args, "--ide")
	}
	if o.Chrome {
		args = append(args, "--chrome")
	}
	if o.NoChrome {
		args = append(args, "--no-chrome")
	}

	// Debug
	if o.Debug != "" {
		args = append(args, "--debug", o.Debug)
	}

	// Session persistence
	if o.NoSessionPersistence {
		args = append(args, "--no-session-persistence")
	}

	// Slash commands
	if o.DisableSlashCommands {
		args = append(args, "--disable-slash-commands")
	}

	// Betas
	if len(o.Betas) > 0 {
		args = append(args, "--betas")
		args = append(args, o.Betas...)
	}

	// Prompt (must be last)
	if o.Prompt != "" {
		args = append(args, o.Prompt)
	}

	return args
}

// Args converts CodexOptions to CLI arguments for `codex exec`.
func (o *CodexOptions) Args() []string {
	var args []string

	// Model
	if o.Model != "" {
		args = append(args, "--model", string(o.Model))
	}

	// Working directory
	if o.WorkDir != "" {
		args = append(args, "--cd", o.WorkDir)
	}

	// Sandbox & approvals
	if o.SandboxMode != "" {
		args = append(args, "--sandbox", string(o.SandboxMode))
	}
	if o.ApprovalPolicy != "" {
		args = append(args, "--ask-for-approval", string(o.ApprovalPolicy))
	}
	if o.FullAuto {
		args = append(args, "--full-auto")
	}
	if o.DangerouslyBypassApprovalsAndSandbox {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}

	// Config overrides
	for k, v := range o.Config {
		args = append(args, "--config", k+"="+v)
	}

	// Features
	for _, f := range o.Enable {
		args = append(args, "--enable", f)
	}
	for _, f := range o.Disable {
		args = append(args, "--disable", f)
	}

	// Profile
	if o.Profile != "" {
		args = append(args, "--profile", o.Profile)
	}

	// OSS/Local
	if o.OSS {
		args = append(args, "--oss")
	}
	if o.LocalProvider != "" {
		args = append(args, "--local-provider", string(o.LocalProvider))
	}

	// Images
	for _, img := range o.Images {
		args = append(args, "--image", img)
	}

	// Output
	if o.JSON {
		args = append(args, "--json")
	}
	if o.OutputLastMessage != "" {
		args = append(args, "--output-last-message", o.OutputLastMessage)
	}
	if o.Color != "" && o.Color != CodexColorAuto {
		args = append(args, "--color", string(o.Color))
	}

	// Schema
	if o.OutputSchema != "" {
		args = append(args, "--output-schema", o.OutputSchema)
	}

	// Git
	if o.SkipGitRepoCheck {
		args = append(args, "--skip-git-repo-check")
	}

	// TUI
	if o.NoAltScreen {
		args = append(args, "--no-alt-screen")
	}

	// Search
	if o.Search {
		args = append(args, "--search")
	}

	// Additional dirs
	for _, dir := range o.AddDirs {
		args = append(args, "--add-dir", dir)
	}

	// Prompt (must be last, or use stdin)
	if o.Prompt != "" {
		args = append(args, o.Prompt)
	}

	return args
}

// =============================================================================
// Helpers
// =============================================================================

func itoa(n int) string {
	return strconv.Itoa(n)
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func joinComma(s []string) string {
	return strings.Join(s, ",")
}

// =============================================================================
// Timing for metrics
// =============================================================================

// SessionMetrics tracks timing for a session.
type SessionMetrics struct {
	StartTime       time.Time
	FirstEventTime  *time.Time
	LastEventTime   *time.Time
	EndTime         *time.Time
	EventCount      int
	ToolCallCount   int
	TokensIn        int
	TokensOut       int
	EstimatedCostUSD float64
}
