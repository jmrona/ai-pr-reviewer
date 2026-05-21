package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/shared"
)

func TestParseModeratorOutputParsesSectionsAndLocations(t *testing.T) {
	output := `TICKET_COVERAGE:
:white_check_mark: All criteria covered

BLOCKERS:
- [Architect] SQL injection risk (api/user.go:42)

WARNINGS:
- [Pragmatist] Missing empty state (collab/page.tsx)

SUGGESTIONS:
- [Designer] Rename getData for clarity

ASSUMPTIONS:
- Acceptance criteria are in the Jira description

SUMMARY:
Fix the blocker before merging.`

	result, err := ParseModeratorOutput(output)
	if err != nil {
		t.Fatalf("ParseModeratorOutput() error = %v", err)
	}

	if result.Blockers[0].File != "api/user.go" || result.Blockers[0].Line != 42 {
		t.Fatalf("blocker location = %s:%d", result.Blockers[0].File, result.Blockers[0].Line)
	}
	if result.Warnings[0].File != "collab/page.tsx" || result.Warnings[0].Line != 0 {
		t.Fatalf("warning location = %s:%d", result.Warnings[0].File, result.Warnings[0].Line)
	}
	if len(result.Assumptions) != 1 {
		t.Fatalf("Assumptions length = %d, want 1", len(result.Assumptions))
	}
}

func TestParseModeratorOutputHandlesNoneSections(t *testing.T) {
	output := `TICKET_COVERAGE:
:warning: Partially covered

BLOCKERS:
None

WARNINGS:
None

SUGGESTIONS:
None

ASSUMPTIONS:
None

SUMMARY:
No findings.`

	result, err := ParseModeratorOutput(output)
	if err != nil {
		t.Fatalf("ParseModeratorOutput() error = %v", err)
	}
	if len(result.Blockers) != 0 || len(result.Warnings) != 0 || len(result.Suggestions) != 0 || len(result.Assumptions) != 0 {
		t.Fatalf("expected empty sections, got %#v", result)
	}
}

func TestParseModeratorOutputHandlesBulletedNoneIssueSections(t *testing.T) {
	output := `TICKET_COVERAGE:
:white_check_mark: All criteria covered

BLOCKERS:
- None

WARNINGS:
- None

SUGGESTIONS:
- None

ASSUMPTIONS:
None

SUMMARY:
No findings.`

	result, err := ParseModeratorOutput(output)
	if err != nil {
		t.Fatalf("ParseModeratorOutput() error = %v", err)
	}
	if len(result.Blockers) != 0 || len(result.Warnings) != 0 || len(result.Suggestions) != 0 {
		t.Fatalf("expected empty issue sections, got %#v", result)
	}
}

func TestParseModeratorOutputParsesInlineSectionValues(t *testing.T) {
	output := `TICKET_COVERAGE: :white_check_mark: All criteria covered

BLOCKERS: None

WARNINGS: None

SUGGESTIONS: None

ASSUMPTIONS: None

SUMMARY: Safe to merge.`

	result, err := ParseModeratorOutput(output)
	if err != nil {
		t.Fatalf("ParseModeratorOutput() error = %v", err)
	}

	if result.TicketCoverage != ":white_check_mark: All criteria covered" {
		t.Fatalf("TicketCoverage = %q", result.TicketCoverage)
	}
	if result.Summary != "Safe to merge." {
		t.Fatalf("Summary = %q", result.Summary)
	}
}

func TestParseModeratorOutputPreservesMixedAgentLabels(t *testing.T) {
	output := `TICKET_COVERAGE:
:white_check_mark: All criteria covered

BLOCKERS:
- [Architect] Cross-slice contract bypasses the boundary (api/auth.go:11)

WARNINGS:
- [Designer] Empty state copy is unclear (collab/auth.tsx:24)

SUGGESTIONS:
- [Pragmatist] Add the missing ticket coverage assertion

ASSUMPTIONS:
None

SUMMARY:
Fix the blocker before merging.`

	result, err := ParseModeratorOutput(output)
	if err != nil {
		t.Fatalf("ParseModeratorOutput() error = %v", err)
	}

	if result.Blockers[0].Agent != "Architect" {
		t.Fatalf("blocker agent = %q, want Architect", result.Blockers[0].Agent)
	}
	if result.Warnings[0].Agent != "Designer" {
		t.Fatalf("warning agent = %q, want Designer", result.Warnings[0].Agent)
	}
	if result.Suggestions[0].Agent != "Pragmatist" {
		t.Fatalf("suggestion agent = %q, want Pragmatist", result.Suggestions[0].Agent)
	}
}

func TestModeratorSkillContainsStableLabelGuardrails(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectSkillsDir, "moderator.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	content := string(data)
	guardrails := []string{
		"Do not default all findings to [Pragmatist]",
		"Use [Architect] for architecture, security, boundaries, coupling, scalability, lifecycle, contracts, and long-term structure.",
		"Use [Designer] for UX, accessibility, product behaviour, naming/readability, copy, visual consistency, and user-facing clarity.",
	}

	for _, guardrail := range guardrails {
		if !strings.Contains(content, guardrail) {
			t.Fatalf("moderator.md does not contain guardrail %q", guardrail)
		}
	}
}

func TestBuildAgentTraceMessageOmitsPromptsWhenPromptCaptureIsDisabled(t *testing.T) {
	message := buildAgentTraceMessage("Pragmatist", "system", "user", "output", false)

	if message.Agent != "Pragmatist" || message.Output != "output" {
		t.Fatalf("trace message = %#v", message)
	}
	if message.SystemPrompt != "" || message.UserPrompt != "" {
		t.Fatalf("prompts = %q/%q, want empty strings", message.SystemPrompt, message.UserPrompt)
	}
}

func TestBuildAgentTraceMessageIncludesPromptsWhenPromptCaptureIsEnabled(t *testing.T) {
	message := buildAgentTraceMessage("Architect", "system", "user", "output", true)

	if message.SystemPrompt != "system" || message.UserPrompt != "user" {
		t.Fatalf("prompts = %q/%q, want populated", message.SystemPrompt, message.UserPrompt)
	}
}

func TestBuildChatCompletionParamsIncludesReasoningEffortWhenConfigured(t *testing.T) {
	params := buildChatCompletionParams("gpt-5.5", "high", "system prompt", "user prompt", 0.3)

	if string(params.Model) != "gpt-5.5" {
		t.Fatalf("Model = %q, want gpt-5.5", params.Model)
	}
	if params.ReasoningEffort != shared.ReasoningEffortHigh {
		t.Fatalf("ReasoningEffort = %q, want high", params.ReasoningEffort)
	}
	if len(params.Messages) != 2 {
		t.Fatalf("Messages length = %d, want 2", len(params.Messages))
	}
}

func TestBuildChatCompletionParamsOmitsReasoningEffortWhenEmpty(t *testing.T) {
	params := buildChatCompletionParams("gpt-4o", "", "system prompt", "user prompt", 0.3)

	if params.ReasoningEffort != "" {
		t.Fatalf("ReasoningEffort = %q, want empty", params.ReasoningEffort)
	}
}

func TestBuildChatCompletionParamsOmitsTemperature(t *testing.T) {
	params := buildChatCompletionParams("gpt-5.5", "high", "system prompt", "user prompt", 0.3)

	if params.Temperature.Valid() {
		t.Fatalf("Temperature = %v, want omitted", params.Temperature)
	}
}

func TestWrapAgentRunErrorPreservesRuntimeErrors(t *testing.T) {
	err := wrapAgentRunError("Architect", errors.New("rate limited"))

	if err == nil {
		t.Fatal("wrapAgentRunError() error = nil, want wrapped error")
	}
	if err.Error() != "running Architect agent: rate limited" {
		t.Fatalf("wrapAgentRunError() = %q", err.Error())
	}
}

func TestEmptyAgentResponseErrorNamesAgent(t *testing.T) {
	err := emptyAgentResponseError("Designer")

	if err == nil {
		t.Fatal("emptyAgentResponseError() error = nil, want empty response error")
	}
	if err.Error() != "running Designer agent: empty response" {
		t.Fatalf("emptyAgentResponseError() = %q", err.Error())
	}
}
