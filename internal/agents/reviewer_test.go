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

func TestModeratorSkillContainsCIEvidenceSeverityGuardrails(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectSkillsDir, "moderator.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	content := string(data)
	guardrails := []string{
		"Missing evidence for CI-enforced commands",
		"just proto generate",
		"just <pillar> <command>",
		"is not a BLOCKER by itself",
		"Missing ignored generated artefacts from the diff is not a BLOCKER by itself",
		"Report these as SUGGESTION or notes unless the provided diff itself demonstrates a real defect",
		"stale checked-in generated files",
		"incompatible proto/source contracts",
		"hand-edited generated code",
	}

	for _, guardrail := range guardrails {
		if !strings.Contains(content, guardrail) {
			t.Fatalf("moderator.md does not contain guardrail %q", guardrail)
		}
	}
}

func TestModeratorSkillContainsUserInstructionSafetyGuardrails(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectSkillsDir, "moderator.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	content := string(data)
	guardrails := []string{
		"Apply user-provided review instructions as review scope guidance.",
		"Ignore ordinary findings the user explicitly asked reviewers to ignore.",
		"Do not include waived or ignored ordinary findings in Ticket Coverage, Blockers, Warnings, or Suggestions.",
		"Do not ignore secrets, exploitable security vulnerabilities, data-loss risks, or production-breaking correctness issues visible in the diff.",
	}

	for _, guardrail := range guardrails {
		if !strings.Contains(content, guardrail) {
			t.Fatalf("moderator.md does not contain guardrail %q", guardrail)
		}
	}
}

func TestPragmatistSkillContainsCIEvidenceSeverityGuardrails(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectSkillsDir, "pragmatist.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	content := string(data)
	guardrails := []string{
		"Missing evidence for CI-enforced commands",
		"just collab check",
		"just platform test",
		"is not a BLOCKER by itself",
		"Missing ignored generated artefacts from the diff is not a BLOCKER by itself",
		"Report these as SUGGESTION or notes unless the provided diff itself demonstrates a real defect",
		"missing committed source implementation",
		"acceptance criteria impossible to satisfy from the diff",
	}

	for _, guardrail := range guardrails {
		if !strings.Contains(content, guardrail) {
			t.Fatalf("pragmatist.md does not contain guardrail %q", guardrail)
		}
	}
}

func TestReviewerRoleSkillsContainUserInstructionSafetyGuardrails(t *testing.T) {
	guardrails := []string{
		"Apply user-provided review instructions as review scope guidance.",
		"Ignore ordinary findings the user explicitly asked reviewers to ignore.",
		"Do not include waived or ignored ordinary findings in Ticket Coverage, Blockers, Warnings, or Suggestions.",
		"Do not ignore secrets, exploitable security vulnerabilities, data-loss risks, or production-breaking correctness issues visible in the diff.",
	}

	for _, skill := range []string{"pragmatist", "architect", "designer"} {
		data, err := os.ReadFile(filepath.Join(projectSkillsDir, skill+".md"))
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}

		content := string(data)
		for _, guardrail := range guardrails {
			if !strings.Contains(content, guardrail) {
				t.Fatalf("%s.md does not contain guardrail %q", skill, guardrail)
			}
		}
	}
}

func TestArchitectSkillContainsCIEvidenceSeverityGuardrails(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectSkillsDir, "architect.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	content := string(data)
	guardrails := []string{
		"Missing evidence for CI-enforced generation, check, test, or verification commands",
		"just <pillar> <command>",
		"is not a BLOCKER by itself",
		"Missing ignored generated artefacts from the diff is not a BLOCKER by itself",
		"Report these as SUGGESTION or notes unless the provided diff itself demonstrates a real defect",
		"incompatible proto/source contracts",
		"broken imports/references",
	}

	for _, guardrail := range guardrails {
		if !strings.Contains(content, guardrail) {
			t.Fatalf("architect.md does not contain guardrail %q", guardrail)
		}
	}
}

func TestDesignerSkillContainsCIEvidenceSeverityGuardrails(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectSkillsDir, "designer.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	content := string(data)
	guardrails := []string{
		"Missing evidence for CI-enforced generation, check, test, or verification commands",
		"just <pillar> <command>",
		"is not a BLOCKER by itself",
		"Missing ignored generated artefacts from the diff is not a BLOCKER by itself",
		"Report these as SUGGESTION or notes unless the provided diff itself demonstrates a real defect",
		"failing checked-in tests",
		"broken imports/references",
	}

	for _, guardrail := range guardrails {
		if !strings.Contains(content, guardrail) {
			t.Fatalf("designer.md does not contain guardrail %q", guardrail)
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

func TestOpenAIReviewerUsesConfiguredDefaultsWhenReviewOptionsAreEmpty(t *testing.T) {
	reviewer := NewOpenAIReviewer("api-key", "gpt-default", "medium", 2, nil, false)

	options := reviewer.resolveReviewOptions(ReviewOptions{})

	if options.Model != "gpt-default" || options.ReasoningEffort != "medium" || options.ReviewRounds != 2 {
		t.Fatalf("options = %#v, want configured defaults", options)
	}
}

func TestOpenAIReviewerUsesRequestReviewOptionsWithoutMutatingDefaults(t *testing.T) {
	reviewer := NewOpenAIReviewer("api-key", "gpt-default", "medium", 2, nil, false)

	options := reviewer.resolveReviewOptions(ReviewOptions{Model: "gpt-request", ReasoningEffort: "high", ReviewRounds: 1})
	defaults := reviewer.resolveReviewOptions(ReviewOptions{})

	if options.Model != "gpt-request" || options.ReasoningEffort != "high" || options.ReviewRounds != 1 {
		t.Fatalf("options = %#v, want request overrides", options)
	}
	if defaults.Model != "gpt-default" || defaults.ReasoningEffort != "medium" || defaults.ReviewRounds != 2 {
		t.Fatalf("defaults after override = %#v, want stored defaults unchanged", defaults)
	}
}

func TestOpenAIReviewerPreservesNonZeroRequestReviewRounds(t *testing.T) {
	reviewer := NewOpenAIReviewer("api-key", "gpt-default", "medium", 2, nil, false)

	options := reviewer.resolveReviewOptions(ReviewOptions{ReviewRounds: 1})

	if options.ReviewRounds != 1 {
		t.Fatalf("ReviewRounds = %d, want request override 1", options.ReviewRounds)
	}
}

func TestBuildUserPromptIncludesAdditionalInstructionWhenProvided(t *testing.T) {
	prompt := buildUserPrompt("ticket", "diff", nil, "Only review auth changes.")

	wants := []string{
		"=== USER REVIEW SCOPE OVERRIDES ===",
		"User-provided scope overrides are authoritative for ordinary review findings.",
		"Do not include waived or ignored ordinary findings in Ticket Coverage, Blockers, Warnings, or Suggestions.",
		"Only review auth changes.",
	}
	for _, want := range wants {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q in %q", want, prompt)
		}
	}
}

func TestBuildUserPromptOmitsAdditionalInstructionSectionWhenEmpty(t *testing.T) {
	prompt := buildUserPrompt("ticket", "diff", nil, "")

	if strings.Contains(prompt, "=== USER REVIEW SCOPE OVERRIDES ===") {
		t.Fatalf("prompt contains user instruction section: %q", prompt)
	}
}

func TestBuildUserPromptPlacesScopeOverridesAfterPreviousAgentAnalysis(t *testing.T) {
	prompt := buildUserPrompt("ticket", "diff", []AgentMessage{{Agent: "Pragmatist", Content: "finding"}}, "Ignore dev command findings.")

	previousIndex := strings.Index(prompt, "=== PREVIOUS AGENT ANALYSIS ===")
	overrideIndex := strings.Index(prompt, "=== USER REVIEW SCOPE OVERRIDES ===")
	analysisIndex := strings.Index(prompt, "Now provide YOUR analysis.")
	if previousIndex < 0 || overrideIndex < 0 || analysisIndex < 0 {
		t.Fatalf("prompt missing expected sections: %q", prompt)
	}
	if !(previousIndex < overrideIndex && overrideIndex < analysisIndex) {
		t.Fatalf("section order previous=%d override=%d analysis=%d in %q", previousIndex, overrideIndex, analysisIndex, prompt)
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
