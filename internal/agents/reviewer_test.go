package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func TestOpenAIReviewerRejectsInvalidResolvedReviewRounds(t *testing.T) {
	reviewer := newFakeReviewer(3, nil)

	_, err := reviewer.Review(context.Background(), "ticket", "diff", false, ReviewOptions{})

	if err == nil {
		t.Fatal("Review() error = nil, want invalid review rounds error")
	}
	if !strings.Contains(err.Error(), "invalid review rounds 3") || !strings.Contains(err.Error(), "1 or 2") {
		t.Fatalf("Review() error = %q, want clear invalid rounds error", err.Error())
	}
}

func TestOpenAIReviewerRunsOneRoundSpecialistsWithoutPriorAnalysisAndModeratesRoundOne(t *testing.T) {
	fake := newRecordingCompletion()
	reviewer := newFakeReviewer(1, fake.complete)

	outcome, err := reviewer.Review(context.Background(), "ticket", "diff", false, ReviewOptions{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	for _, role := range []string{"pragmatist", "architect", "designer"} {
		prompt := fake.specialistPrompt(t, role, 1)
		if strings.Contains(prompt, "PRIOR ROUND AGENT ANALYSIS") || strings.Contains(prompt, "PREVIOUS AGENT ANALYSIS") {
			t.Fatalf("round 1 %s prompt contains prior analysis: %q", role, prompt)
		}
	}

	moderatorPrompt := fake.moderatorPrompt(t)
	for _, want := range []string{"=== FINAL SPECIALIST AGENT ANALYSIS ===", "Pragmatist:\nround 1 pragmatist", "Architect:\nround 1 architect", "Designer:\nround 1 designer"} {
		if !strings.Contains(moderatorPrompt, want) {
			t.Fatalf("moderator prompt missing %q in %q", want, moderatorPrompt)
		}
	}
	if outcome.Trace.ReviewRounds != 1 {
		t.Fatalf("Trace.ReviewRounds = %d, want 1", outcome.Trace.ReviewRounds)
	}
}

func TestOpenAIReviewerRunsTwoRoundSpecialistsWithRoundOneContextAndModeratesFinalRoundOnly(t *testing.T) {
	fake := newRecordingCompletion()
	reviewer := newFakeReviewer(2, fake.complete)

	outcome, err := reviewer.Review(context.Background(), "ticket", "diff", false, ReviewOptions{})
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}

	for _, role := range []string{"pragmatist", "architect", "designer"} {
		prompt := fake.specialistPrompt(t, role, 2)
		assertInOrder(t, prompt, []string{
			"=== PRIOR ROUND AGENT ANALYSIS ===",
			"Pragmatist:\nround 1 pragmatist",
			"Architect:\nround 1 architect",
			"Designer:\nround 1 designer",
			"Now provide YOUR analysis.",
		})
	}

	moderatorPrompt := fake.moderatorPrompt(t)
	for _, want := range []string{"=== FINAL SPECIALIST AGENT ANALYSIS ===", "Pragmatist:\nround 2 pragmatist", "Architect:\nround 2 architect", "Designer:\nround 2 designer"} {
		if !strings.Contains(moderatorPrompt, want) {
			t.Fatalf("moderator prompt missing %q in %q", want, moderatorPrompt)
		}
	}
	for _, unwanted := range []string{"round 1 pragmatist", "round 1 architect", "round 1 designer"} {
		if strings.Contains(moderatorPrompt, unwanted) {
			t.Fatalf("moderator prompt contains non-final output %q in %q", unwanted, moderatorPrompt)
		}
	}

	wantTraceAgents := []string{"Round 1 - Pragmatist", "Round 1 - Architect", "Round 1 - Designer", "Round 2 - Pragmatist", "Round 2 - Architect", "Round 2 - Designer", "Moderator"}
	if got := traceAgentNames(outcome.Trace.AgentMessages); strings.Join(got, ",") != strings.Join(wantTraceAgents, ",") {
		t.Fatalf("trace agents = %#v, want %#v", got, wantTraceAgents)
	}

	wantDebate := []AgentMessage{{Agent: "Pragmatist", Content: "round 2 pragmatist"}, {Agent: "Architect", Content: "round 2 architect"}, {Agent: "Designer", Content: "round 2 designer"}}
	if len(outcome.Result.AgentDebate) != len(wantDebate) {
		t.Fatalf("AgentDebate length = %d, want %d", len(outcome.Result.AgentDebate), len(wantDebate))
	}
	for i, want := range wantDebate {
		if outcome.Result.AgentDebate[i] != want {
			t.Fatalf("AgentDebate[%d] = %#v, want %#v", i, outcome.Result.AgentDebate[i], want)
		}
	}
}

func TestOpenAIReviewerStopsBeforeLaterRoundsAndModeratorWhenSpecialistFails(t *testing.T) {
	fake := newRecordingCompletion()
	fake.failRole = "architect"
	reviewer := newFakeReviewer(2, fake.complete)

	_, err := reviewer.Review(context.Background(), "ticket", "diff", false, ReviewOptions{})

	if err == nil {
		t.Fatal("Review() error = nil, want specialist failure")
	}
	if !strings.Contains(err.Error(), "round 1") || !strings.Contains(err.Error(), "Architect") {
		t.Fatalf("Review() error = %q, want round and agent", err.Error())
	}
	if fake.countRoleRound("moderator", 1) != 0 {
		t.Fatalf("moderator was called after specialist failure")
	}
	for _, role := range []string{"pragmatist", "architect", "designer"} {
		if fake.countRoleRound(role, 2) != 0 {
			t.Fatalf("round 2 %s was called after round 1 failure", role)
		}
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

	previousIndex := strings.Index(prompt, "=== PRIOR ROUND AGENT ANALYSIS ===")
	overrideIndex := strings.Index(prompt, "=== USER REVIEW SCOPE OVERRIDES ===")
	analysisIndex := strings.Index(prompt, "Now provide YOUR analysis.")
	if previousIndex < 0 || overrideIndex < 0 || analysisIndex < 0 {
		t.Fatalf("prompt missing expected sections: %q", prompt)
	}
	if !(previousIndex < overrideIndex && overrideIndex < analysisIndex) {
		t.Fatalf("section order previous=%d override=%d analysis=%d in %q", previousIndex, overrideIndex, analysisIndex, prompt)
	}
}

func TestBuildModeratorPromptPlacesScopeOverridesAfterFinalSpecialistAnalysis(t *testing.T) {
	prompt := buildModeratorUserPrompt("ticket", "diff", []AgentMessage{{Agent: "Pragmatist", Content: "finding"}}, "Ignore dev command findings.")

	finalIndex := strings.Index(prompt, "=== FINAL SPECIALIST AGENT ANALYSIS ===")
	overrideIndex := strings.Index(prompt, "=== USER REVIEW SCOPE OVERRIDES ===")
	analysisIndex := strings.Index(prompt, "Now provide YOUR analysis.")
	if finalIndex < 0 || overrideIndex < 0 || analysisIndex < 0 {
		t.Fatalf("prompt missing expected sections: %q", prompt)
	}
	if !(finalIndex < overrideIndex && overrideIndex < analysisIndex) {
		t.Fatalf("section order final=%d override=%d analysis=%d in %q", finalIndex, overrideIndex, analysisIndex, prompt)
	}
}

func newFakeReviewer(reviewRounds int, complete chatCompletionFunc) *OpenAIReviewer {
	reviewer := NewOpenAIReviewer("api-key", "gpt-default", "medium", reviewRounds, map[string]string{
		"non_interactive": "non interactive",
		"pragmatist":      "pragmatist system",
		"architect":       "architect system",
		"designer":        "designer system",
		"moderator":       "moderator system",
	}, true)
	if complete != nil {
		reviewer.complete = complete
	}
	return reviewer
}

type recordedCompletionCall struct {
	role       string
	round      int
	userPrompt string
}

type recordingCompletion struct {
	mu       sync.Mutex
	calls    []recordedCompletionCall
	failRole string
}

func newRecordingCompletion() *recordingCompletion {
	return &recordingCompletion{}
}

func (f *recordingCompletion) complete(ctx context.Context, model, reasoningEffort, systemPrompt, userPrompt string, temperature float32) (string, error) {
	role := roleFromSystemPrompt(systemPrompt)
	round := 1
	if role != "moderator" && strings.Contains(userPrompt, "PRIOR ROUND AGENT ANALYSIS") {
		round = 2
	}

	f.mu.Lock()
	f.calls = append(f.calls, recordedCompletionCall{role: role, round: round, userPrompt: userPrompt})
	f.mu.Unlock()

	if role == f.failRole {
		return "", errors.New("boom")
	}
	if role == "moderator" {
		return validModeratorOutput(), nil
	}
	return "round " + strconv.Itoa(round) + " " + role, nil
}

func (f *recordingCompletion) specialistPrompt(t *testing.T, role string, round int) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call.role == role && call.round == round {
			return call.userPrompt
		}
	}
	t.Fatalf("missing %s round %d call in %#v", role, round, f.calls)
	return ""
}

func (f *recordingCompletion) moderatorPrompt(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call.role == "moderator" {
			return call.userPrompt
		}
	}
	t.Fatalf("missing moderator call in %#v", f.calls)
	return ""
}

func (f *recordingCompletion) countRoleRound(role string, round int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if call.role == role && call.round == round {
			count++
		}
	}
	return count
}

func roleFromSystemPrompt(systemPrompt string) string {
	for _, role := range []string{"pragmatist", "architect", "designer", "moderator"} {
		if strings.Contains(systemPrompt, role+" system") {
			return role
		}
	}
	return "unknown"
}

func validModeratorOutput() string {
	return `TICKET_COVERAGE: :white_check_mark: All criteria covered

BLOCKERS: None

WARNINGS: None

SUGGESTIONS: None

ASSUMPTIONS: None

SUMMARY: Safe to merge.`
}

func assertInOrder(t *testing.T, content string, wants []string) {
	t.Helper()
	last := -1
	for _, want := range wants {
		idx := strings.Index(content, want)
		if idx < 0 {
			t.Fatalf("content missing %q in %q", want, content)
		}
		if idx <= last {
			t.Fatalf("content has %q out of order in %q", want, content)
		}
		last = idx
	}
}

func traceAgentNames(messages []AgentTraceMessage) []string {
	names := make([]string, 0, len(messages))
	for _, message := range messages {
		names = append(names, message.Agent)
	}
	return names
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
