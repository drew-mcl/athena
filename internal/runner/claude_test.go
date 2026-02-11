package runner

import "testing"

func TestBuildClaudeOptions_EnvPropagation(t *testing.T) {
	spec := RunSpec{
		SessionID: "test-session",
		WorkDir:   "/tmp/test",
		Prompt:    "Hello",
		Env: map[string]string{
			"CLAUDE_CODE_TASK_LIST_ID": "wi-1234",
			"CUSTOM_VAR":              "value",
		},
	}

	opts := buildClaudeOptions(spec, false)

	if opts.Env == nil {
		t.Fatal("expected Env to be set")
	}
	if opts.Env["CLAUDE_CODE_TASK_LIST_ID"] != "wi-1234" {
		t.Errorf("expected CLAUDE_CODE_TASK_LIST_ID=wi-1234, got %q", opts.Env["CLAUDE_CODE_TASK_LIST_ID"])
	}
	if opts.Env["CUSTOM_VAR"] != "value" {
		t.Errorf("expected CUSTOM_VAR=value, got %q", opts.Env["CUSTOM_VAR"])
	}
}

func TestBuildClaudeOptions_NilEnv(t *testing.T) {
	spec := RunSpec{
		SessionID: "test-session",
		WorkDir:   "/tmp/test",
		Prompt:    "Hello",
	}

	opts := buildClaudeOptions(spec, false)

	if opts.Env != nil {
		t.Errorf("expected nil Env when RunSpec.Env is nil, got %v", opts.Env)
	}
}

func TestBuildClaudeOptions_EmptyEnv(t *testing.T) {
	spec := RunSpec{
		SessionID: "test-session",
		WorkDir:   "/tmp/test",
		Prompt:    "Hello",
		Env:       map[string]string{},
	}

	opts := buildClaudeOptions(spec, false)

	if len(opts.Env) != 0 {
		t.Errorf("expected empty Env, got %v", opts.Env)
	}
}

func TestBuildClaudeOptions_ResumeMode(t *testing.T) {
	spec := RunSpec{
		SessionID: "test-session",
		WorkDir:   "/tmp/test",
		Prompt:    "should be cleared",
		Env: map[string]string{
			"CLAUDE_CODE_TASK_LIST_ID": "wi-5678",
		},
	}

	opts := buildClaudeOptions(spec, true)

	// Env should still be propagated in resume mode
	if opts.Env["CLAUDE_CODE_TASK_LIST_ID"] != "wi-5678" {
		t.Errorf("expected env to propagate in resume mode")
	}

	// Prompt should be cleared in resume mode
	if opts.Prompt != "" {
		t.Errorf("expected prompt to be empty in resume mode, got %q", opts.Prompt)
	}
}

func TestBuildClaudeOptions_GitIdentityWithEnv(t *testing.T) {
	spec := RunSpec{
		SessionID: "test-session",
		WorkDir:   "/tmp/test",
		Prompt:    "Hello",
		Env: map[string]string{
			"CLAUDE_CODE_TASK_LIST_ID": "wi-1234",
		},
		GitIdentity: &GitIdentityConfig{
			AuthorName:  "Test Bot",
			AuthorEmail: "bot@test.com",
		},
	}

	opts := buildClaudeOptions(spec, false)

	// Both Env and GitIdentity should be set
	if opts.Env["CLAUDE_CODE_TASK_LIST_ID"] != "wi-1234" {
		t.Errorf("expected env to be set alongside git identity")
	}
	if opts.GitIdentity == nil {
		t.Fatal("expected git identity to be set")
	}
	if opts.GitIdentity.AuthorName != "Test Bot" {
		t.Errorf("expected author name 'Test Bot', got %q", opts.GitIdentity.AuthorName)
	}
}
