package dashboard

import (
	"testing"
	"time"

	"github.com/drewfead/athena/internal/control"
)

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{123, "123"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1234567, "1,234,567"},
	}

	for _, tt := range tests {
		result := formatTokenCount(tt.input)
		if result != tt.expected {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFormatDurationHuman(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1.5s"},
		{30 * time.Second, "30.0s"},
		{90 * time.Second, "1m 30s"},
		{5*time.Minute + 30*time.Second, "5m 30s"},
		{2*time.Hour + 30*time.Minute, "2h 30m"},
	}

	for _, tt := range tests {
		result := formatDurationHuman(tt.input)
		if result != tt.expected {
			t.Errorf("formatDurationHuman(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestRenderProgressBar(t *testing.T) {
	// Test that progress bar doesn't panic on edge cases
	renderProgressBar(0, 10)
	renderProgressBar(1.0, 10)
	renderProgressBar(0.5, 10)
	renderProgressBar(0.5, 0)
	renderProgressBar(-0.1, 10)
	renderProgressBar(1.5, 10)
}

func TestRenderCompactMetrics(t *testing.T) {
	// Nil metrics should return empty string
	result := renderCompactMetrics(nil)
	if result != "" {
		t.Errorf("renderCompactMetrics(nil) = %q, want empty string", result)
	}

	// Metrics with data should return non-empty
	metrics := &control.AgentMetrics{
		TotalTokens:  1000,
		ToolUseCount: 5,
		DurationMs:   60000,
	}
	result = renderCompactMetrics(metrics)
	if result == "" {
		t.Error("renderCompactMetrics with data should return non-empty string")
	}
}

func TestRenderFileActivity(t *testing.T) {
	// Nil metrics
	result := renderFileActivity(nil)
	if result != "" {
		t.Errorf("renderFileActivity(nil) = %q, want empty string", result)
	}

	// Zero values
	metrics := &control.AgentMetrics{}
	result = renderFileActivity(metrics)
	if result != "" {
		t.Errorf("renderFileActivity(zero) = %q, want empty string", result)
	}

	// With values
	metrics = &control.AgentMetrics{
		FilesRead:    10,
		FilesWritten: 5,
		LinesChanged: 100,
	}
	result = renderFileActivity(metrics)
	if result == "" {
		t.Error("renderFileActivity with data should return non-empty string")
	}
}
