package trace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/ai-pr-reviewer/internal/agents"
)

func TestWriterCreatesDirectoryAndFileWithSanitisedFilenameAndSections(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "traces")
	createdAt := time.Date(2026, 5, 21, 14, 30, 45, 0, time.FixedZone("PDT", -7*60*60))
	writer := Writer{Enabled: true, Dir: dir, IncludePrompts: true}

	path, err := writer.Write(context.Background(), TraceInput{
		IssueKey:              "AI PR/123",
		MRURL:                 "https://gitlab.example.com/project/-/merge_requests/7",
		TicketURL:             "https://jira.example.com/browse/AI-123",
		AdditionalInstruction: "Ignore generated files.",
		TicketContext:         "ticket context",
		Diff:                  "diff content",
		DiffTruncated:         true,
		ReviewOutcome:         reviewOutcome(true),
		SlackMessage:          "slack message",
		CreatedAt:             createdAt,
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	wantPath := filepath.Join(dir, "AI-PR-123-20260521T213045Z.md")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(contentBytes)

	for _, want := range []string{
		"# Review Trace: AI PR/123",
		"## Metadata",
		"Ticket: AI PR/123",
		"MR URL: https://gitlab.example.com/project/-/merge_requests/7",
		"Ticket URL: https://jira.example.com/browse/AI-123",
		"Additional instruction: Ignore generated files.",
		"Timestamp: 2026-05-21T21:30:45Z",
		"Model: gpt-test",
		"Reasoning effort: high",
		"Review rounds: 2",
		"Diff truncated: true",
		"Prompt capture: enabled",
		"## Ticket Context",
		"ticket context",
		"## MR Diff",
		"diff content",
		"## Prompts",
		"## Agent Outputs",
		"### Round 2 - Pragmatist",
		"agent output",
		"## Moderator Output",
		"moderator output",
		"## Parsed Review Result",
		"coverage",
		"## Final Slack Message",
		"slack message",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace content missing %q:\n%s", want, content)
		}
	}
}

func TestWriterDisabledWritesNothingAndReturnsEmptyPath(t *testing.T) {
	dir := t.TempDir()
	writer := Writer{Enabled: false, Dir: dir, IncludePrompts: true}

	path, err := writer.Write(context.Background(), TraceInput{
		IssueKey:      "AI-123",
		ReviewOutcome: reviewOutcome(true),
		CreatedAt:     time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty", path)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries length = %d, want 0", len(entries))
	}
}

func TestWriterWithPromptsDisabledWritesDisabledNoticeAndOmitsPromptContent(t *testing.T) {
	writer := Writer{Enabled: true, Dir: t.TempDir(), IncludePrompts: false}

	path, err := writer.Write(context.Background(), TraceInput{
		IssueKey:      "AI-123",
		ReviewOutcome: reviewOutcome(true),
		CreatedAt:     time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(contentBytes)

	if !strings.Contains(content, "Prompt capture disabled.") {
		t.Fatalf("trace content missing disabled notice:\n%s", content)
	}
	if strings.Contains(content, "system prompt") || strings.Contains(content, "user prompt") {
		t.Fatalf("trace content contains prompt content when disabled:\n%s", content)
	}
}

func TestWriterWithPromptsEnabledWritesPromptSectionsAndContent(t *testing.T) {
	writer := Writer{Enabled: true, Dir: t.TempDir(), IncludePrompts: true}

	path, err := writer.Write(context.Background(), TraceInput{
		IssueKey:      "AI-123",
		ReviewOutcome: reviewOutcome(true),
		CreatedAt:     time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(contentBytes)

	for _, want := range []string{"### Round 2 - Pragmatist", "#### System Prompt", "system prompt", "#### User Prompt", "user prompt"} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace content missing %q:\n%s", want, content)
		}
	}
}

func TestWriterRendersAgentDurationsNearPromptsAndOutputs(t *testing.T) {
	writer := Writer{Enabled: true, Dir: t.TempDir(), IncludePrompts: true}

	path, err := writer.Write(context.Background(), TraceInput{
		IssueKey: "AI-123",
		ReviewOutcome: agents.ReviewOutcome{
			Trace: agents.ReviewTrace{
				AgentMessages: []agents.AgentTraceMessage{
					{Agent: "Round 2 - Pragmatist", SystemPrompt: "system prompt", UserPrompt: "user prompt", Output: "agent output", Duration: 4200 * time.Millisecond},
					{Agent: "Moderator", SystemPrompt: "moderator system", UserPrompt: "moderator user", Output: "moderator output"},
				},
			},
		},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(contentBytes)

	assertInOrder(t, content, []string{"## Prompts", "### Round 2 - Pragmatist", "Duration: 4.2s", "#### System Prompt"})
	assertInOrder(t, content, []string{"## Agent Outputs", "### Round 2 - Pragmatist", "Duration: 4.2s", "agent output"})
	if strings.Contains(sectionForAgent(t, content, "## Prompts", "Moderator"), "Duration:") {
		t.Fatalf("zero duration prompt section contains duration:\n%s", content)
	}
	if strings.Contains(sectionForAgent(t, content, "## Agent Outputs", "Moderator"), "Duration:") {
		t.Fatalf("zero duration output section contains duration:\n%s", content)
	}
}

func TestWriterRedactsConfiguredSecretsSlackHooksAuthorizationLinesAndBearerTokens(t *testing.T) {
	writer := Writer{Enabled: true, Dir: t.TempDir(), IncludePrompts: true, Redactions: []string{"configured-secret"}}

	path, err := writer.Write(context.Background(), TraceInput{
		IssueKey:      "AI-123",
		TicketContext: "configured-secret https://hooks.slack.com/services/T000/B000/secret Authorization: Basic abc123\nBearer abc.def-123_456",
		ReviewOutcome: agents.ReviewOutcome{
			Result: agents.ReviewResult{Summary: "configured-secret"},
			Trace: agents.ReviewTrace{
				Model: "gpt-test",
				AgentMessages: []agents.AgentTraceMessage{
					{Agent: "Pragmatist", SystemPrompt: "Authorization: Bearer secret", UserPrompt: "Bearer token-value", Output: "https://hooks.slack.com/services/T000/B000/output"},
				},
			},
		},
		SlackMessage: "configured-secret Bearer slack-token",
		CreatedAt:    time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(contentBytes)

	for _, leaked := range []string{"configured-secret", "hooks.slack.com/services", "Authorization:", "Basic abc123", "Bearer abc.def", "Bearer token-value", "Bearer slack-token"} {
		if strings.Contains(content, leaked) {
			t.Fatalf("trace content leaked %q:\n%s", leaked, content)
		}
	}
	if count := strings.Count(content, "[REDACTED]"); count < 6 {
		t.Fatalf("redaction count = %d, want at least 6:\n%s", count, content)
	}
}

func reviewOutcome(includePrompts bool) agents.ReviewOutcome {
	message := agents.AgentTraceMessage{Agent: "Round 2 - Pragmatist", Output: "agent output"}
	if includePrompts {
		message.SystemPrompt = "system prompt"
		message.UserPrompt = "user prompt"
	}

	return agents.ReviewOutcome{
		Result: agents.ReviewResult{
			TicketCoverage: "coverage",
			Blockers: []agents.Issue{
				{Agent: "Architect", Severity: "BLOCKER", Description: "blocker", File: "file.go", Line: 7},
			},
			Warnings: []agents.Issue{
				{Agent: "Designer", Severity: "WARNING", Description: "warning"},
			},
			Suggestions: []agents.Issue{
				{Agent: "Pragmatist", Severity: "SUGGESTION", Description: "suggestion"},
			},
			Assumptions: []string{"assumption"},
			Summary:     "summary",
		},
		Trace: agents.ReviewTrace{
			Model:           "gpt-test",
			ReasoningEffort: "high",
			ReviewRounds:    2,
			IncludePrompts:  includePrompts,
			AgentMessages:   []agents.AgentTraceMessage{message},
			ModeratorOutput: "moderator output",
		},
	}
}

func assertInOrder(t *testing.T, content string, wants []string) {
	t.Helper()
	last := -1
	for _, want := range wants {
		idx := strings.Index(content[last+1:], want)
		if idx < 0 {
			t.Fatalf("content missing %q after offset %d:\n%s", want, last, content)
		}
		last += idx + 1
	}
}

func sectionForAgent(t *testing.T, content, parentSection, agent string) string {
	t.Helper()
	parentStart := strings.Index(content, parentSection)
	if parentStart < 0 {
		t.Fatalf("missing parent section %q:\n%s", parentSection, content)
	}
	agentHeading := "### " + agent
	agentStart := strings.Index(content[parentStart:], agentHeading)
	if agentStart < 0 {
		t.Fatalf("missing agent section %q after %q:\n%s", agent, parentSection, content)
	}
	start := parentStart + agentStart
	nextAgent := strings.Index(content[start+len(agentHeading):], "\n### ")
	nextParent := strings.Index(content[start+len(agentHeading):], "\n## ")
	end := len(content)
	for _, next := range []int{nextAgent, nextParent} {
		if next >= 0 && start+len(agentHeading)+next < end {
			end = start + len(agentHeading) + next
		}
	}
	return content[start:end]
}
