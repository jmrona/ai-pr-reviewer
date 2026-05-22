package agents

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type Issue struct {
	Agent       string
	Severity    string
	Description string
	File        string
	Line        int
}

type AgentMessage struct {
	Agent   string
	Content string
}

type ReviewResult struct {
	TicketCoverage string
	Blockers       []Issue
	Warnings       []Issue
	Suggestions    []Issue
	AgentDebate    []AgentMessage
	Summary        string
	Assumptions    []string
	DiffTruncated  bool
}

type ReviewOutcome struct {
	Result ReviewResult
	Trace  ReviewTrace
}

type ReviewOptions struct {
	Model                 string
	ReasoningEffort       string
	ReviewRounds          int
	AdditionalInstruction string
}

type ReviewTrace struct {
	Model           string
	ReasoningEffort string
	ReviewRounds    int
	IncludePrompts  bool
	AgentMessages   []AgentTraceMessage
	ModeratorOutput string
}

type AgentTraceMessage struct {
	Agent        string
	SystemPrompt string
	UserPrompt   string
	Output       string
}

type OpenAIReviewer struct {
	client          openai.Client
	model           string
	reasoningEffort string
	reviewRounds    int
	skills          map[string]string
	includePrompts  bool
	complete        chatCompletionFunc
}

type chatCompletionFunc func(ctx context.Context, model, reasoningEffort, systemPrompt, userPrompt string, temperature float32) (string, error)

type specialistRole struct {
	agent string
	role  string
}

var specialistRoles = []specialistRole{
	{agent: "Pragmatist", role: "pragmatist"},
	{agent: "Architect", role: "architect"},
	{agent: "Designer", role: "designer"},
}

func NewOpenAIReviewer(apiKey, model, reasoningEffort string, reviewRounds int, skills map[string]string, includePrompts bool) *OpenAIReviewer {
	reviewer := &OpenAIReviewer{
		client:          openai.NewClient(option.WithAPIKey(apiKey)),
		model:           model,
		reasoningEffort: reasoningEffort,
		reviewRounds:    reviewRounds,
		skills:          skills,
		includePrompts:  includePrompts,
	}
	reviewer.complete = reviewer.openAICompletion
	return reviewer
}

func (r *OpenAIReviewer) Review(ctx context.Context, ticketContext, diff string, diffTruncated bool, options ReviewOptions) (ReviewOutcome, error) {
	effectiveOptions := r.resolveReviewOptions(options)
	if effectiveOptions.ReviewRounds != 1 && effectiveOptions.ReviewRounds != 2 {
		return ReviewOutcome{}, fmt.Errorf("invalid review rounds %d: expected 1 or 2", effectiveOptions.ReviewRounds)
	}

	trace := ReviewTrace{Model: effectiveOptions.Model, ReasoningEffort: effectiveOptions.ReasoningEffort, ReviewRounds: effectiveOptions.ReviewRounds, IncludePrompts: r.includePrompts, AgentMessages: make([]AgentTraceMessage, 0, effectiveOptions.ReviewRounds*3+1)}

	var finalRound []AgentMessage
	var priorRound []AgentMessage
	for round := 1; round <= effectiveOptions.ReviewRounds; round++ {
		messages, traces, err := r.runSpecialistRound(ctx, round, ticketContext, diff, priorRound, effectiveOptions)
		if err != nil {
			return ReviewOutcome{Trace: trace}, err
		}
		trace.AgentMessages = append(trace.AgentMessages, traces...)
		finalRound = messages
		priorRound = messages
	}

	moderator, moderatorTrace, err := r.runAgent(ctx, "Moderator", "Moderator", "moderator", ticketContext, diff, finalRound, "=== FINAL SPECIALIST AGENT ANALYSIS ===", 0.1, effectiveOptions)
	if err != nil {
		return ReviewOutcome{Trace: trace}, err
	}
	trace.AgentMessages = append(trace.AgentMessages, moderatorTrace)
	trace.ModeratorOutput = moderator

	result, err := ParseModeratorOutput(moderator)
	if err != nil {
		return ReviewOutcome{Trace: trace}, fmt.Errorf("parsing moderator output: %w", err)
	}
	result.AgentDebate = finalRound
	result.DiffTruncated = diffTruncated

	return ReviewOutcome{Result: result, Trace: trace}, nil
}

func (r *OpenAIReviewer) resolveReviewOptions(options ReviewOptions) ReviewOptions {
	effective := options
	if effective.Model == "" {
		effective.Model = r.model
	}
	if effective.ReasoningEffort == "" {
		effective.ReasoningEffort = r.reasoningEffort
	}
	if effective.ReviewRounds == 0 {
		effective.ReviewRounds = r.reviewRounds
	}
	return effective
}

func (r *OpenAIReviewer) runSpecialistRound(ctx context.Context, round int, ticketContext, diff string, previous []AgentMessage, options ReviewOptions) ([]AgentMessage, []AgentTraceMessage, error) {
	roundCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	messages := make([]AgentMessage, len(specialistRoles))
	traces := make([]AgentTraceMessage, len(specialistRoles))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, role := range specialistRoles {
		i, role := i, role
		wg.Add(1)
		go func() {
			defer wg.Done()
			output, trace, err := r.runAgent(roundCtx, role.agent, fmt.Sprintf("Round %d - %s", round, role.agent), role.role, ticketContext, diff, previous, "=== PRIOR ROUND AGENT ANALYSIS ===", 0.3, options)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("round %d %s agent: %w", round, role.agent, err)
				}
				mu.Unlock()
				cancel()
				return
			}
			messages[i] = AgentMessage{Agent: role.agent, Content: output}
			traces[i] = trace
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return nil, nil, firstErr
	}
	return messages, traces, nil
}

func (r *OpenAIReviewer) runAgent(ctx context.Context, agent, traceAgent, role, ticketContext, diff string, previous []AgentMessage, analysisHeader string, temperature float32, options ReviewOptions) (string, AgentTraceMessage, error) {
	systemPrompt, err := ComposeSystemPrompt(r.skills, role)
	if err != nil {
		return "", AgentTraceMessage{}, err
	}
	userPrompt := buildPrompt(ticketContext, diff, previous, analysisHeader, options.AdditionalInstruction)

	output, err := r.complete(ctx, options.Model, options.ReasoningEffort, systemPrompt, userPrompt, temperature)
	if err != nil {
		return "", AgentTraceMessage{}, wrapAgentRunError(agent, err)
	}

	return output, buildAgentTraceMessage(traceAgent, systemPrompt, userPrompt, output, r.includePrompts), nil
}

func (r *OpenAIReviewer) openAICompletion(ctx context.Context, model, reasoningEffort, systemPrompt, userPrompt string, temperature float32) (string, error) {
	resp, err := r.client.Chat.Completions.New(ctx, buildChatCompletionParams(model, reasoningEffort, systemPrompt, userPrompt, float64(temperature)))
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

func buildChatCompletionParams(model, reasoningEffort, systemPrompt, userPrompt string, temperature float64) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModel(model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	}
	if reasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(reasoningEffort)
	}
	return params
}

func wrapAgentRunError(agent string, err error) error {
	return fmt.Errorf("running %s agent: %w", agent, err)
}

func emptyAgentResponseError(agent string) error {
	return fmt.Errorf("running %s agent: empty response", agent)
}

func buildAgentTraceMessage(agent, systemPrompt, userPrompt, output string, includePrompts bool) AgentTraceMessage {
	message := AgentTraceMessage{Agent: agent, Output: output}
	if includePrompts {
		message.SystemPrompt = systemPrompt
		message.UserPrompt = userPrompt
	}
	return message
}

func buildUserPrompt(ticketContext, diff string, previous []AgentMessage, additionalInstruction string) string {
	return buildPrompt(ticketContext, diff, previous, "=== PRIOR ROUND AGENT ANALYSIS ===", additionalInstruction)
}

func buildModeratorUserPrompt(ticketContext, diff string, final []AgentMessage, additionalInstruction string) string {
	return buildPrompt(ticketContext, diff, final, "=== FINAL SPECIALIST AGENT ANALYSIS ===", additionalInstruction)
}

func buildPrompt(ticketContext, diff string, previous []AgentMessage, analysisHeader, additionalInstruction string) string {
	var b strings.Builder
	b.WriteString("=== JIRA TICKET ===\n")
	b.WriteString(ticketContext)
	b.WriteString("\n\n=== MR DIFF ===\n")
	b.WriteString(diff)

	if len(previous) > 0 {
		b.WriteString("\n\n")
		b.WriteString(analysisHeader)
		b.WriteString("\n")
		for _, msg := range previous {
			b.WriteString(msg.Agent)
			b.WriteString(":\n")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")
		}
	}

	if additionalInstruction != "" {
		b.WriteString("\n\n=== USER REVIEW SCOPE OVERRIDES ===\n")
		b.WriteString("User-provided scope overrides are authoritative for ordinary review findings.\n")
		b.WriteString("If the user explicitly asks to ignore or waive a requirement, do not count that waived item as missing ticket coverage.\n")
		b.WriteString("Do not include waived or ignored ordinary findings in Ticket Coverage, Blockers, Warnings, or Suggestions.\n")
		b.WriteString("Do not suppress secrets, exploitable security vulnerabilities, data-loss risks, or production-breaking correctness issues visible in the diff.\n")
		b.WriteString(additionalInstruction)
	}

	b.WriteString("\nNow provide YOUR analysis.")
	return b.String()
}

func ParseModeratorOutput(output string) (ReviewResult, error) {
	sections := map[string][]string{}
	current := ""

	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if section, value, ok := parseSectionHeader(line); ok {
			current = section
			sections[current] = []string{}
			if value != "" {
				sections[current] = append(sections[current], value)
			}
			continue
		}

		if current != "" && line != "" {
			sections[current] = append(sections[current], line)
		}
	}

	for _, name := range []string{"TICKET_COVERAGE", "BLOCKERS", "WARNINGS", "SUGGESTIONS", "ASSUMPTIONS", "SUMMARY"} {
		if _, ok := sections[name]; !ok {
			return ReviewResult{}, fmt.Errorf("missing section %s", name)
		}
	}

	blockers, err := parseIssueSection("BLOCKER", sections["BLOCKERS"])
	if err != nil {
		return ReviewResult{}, err
	}
	warnings, err := parseIssueSection("WARNING", sections["WARNINGS"])
	if err != nil {
		return ReviewResult{}, err
	}
	suggestions, err := parseIssueSection("SUGGESTION", sections["SUGGESTIONS"])
	if err != nil {
		return ReviewResult{}, err
	}

	return ReviewResult{
		TicketCoverage: strings.Join(sections["TICKET_COVERAGE"], "\n"),
		Blockers:       blockers,
		Warnings:       warnings,
		Suggestions:    suggestions,
		Assumptions:    parseTextSection(sections["ASSUMPTIONS"]),
		Summary:        strings.Join(sections["SUMMARY"], "\n"),
	}, nil
}

func parseSectionHeader(line string) (string, string, bool) {
	for _, section := range []string{"TICKET_COVERAGE", "BLOCKERS", "WARNINGS", "SUGGESTIONS", "ASSUMPTIONS", "SUMMARY"} {
		prefix := section + ":"
		if line == prefix {
			return section, "", true
		}
		if strings.HasPrefix(line, prefix+" ") {
			return section, strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
		}
	}
	return "", "", false
}

func parseTextSection(lines []string) []string {
	if isNoneSection(lines) {
		return nil
	}

	values := make([]string, 0, len(lines))
	for _, line := range lines {
		values = append(values, strings.TrimPrefix(line, "- "))
	}
	return values
}

func parseIssueSection(severity string, lines []string) ([]Issue, error) {
	if isNoneSection(lines) {
		return nil, nil
	}

	issues := make([]Issue, 0, len(lines))
	for _, line := range lines {
		issue, err := parseIssueLine(severity, line)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

func isNoneSection(lines []string) bool {
	return len(lines) == 1 && strings.TrimPrefix(lines[0], "- ") == "None"
}

func parseIssueLine(severity, line string) (Issue, error) {
	if !strings.HasPrefix(line, "- [") {
		return Issue{}, fmt.Errorf("invalid issue line %q", line)
	}

	endAgent := strings.Index(line, "] ")
	if endAgent < 0 {
		return Issue{}, fmt.Errorf("invalid issue agent in %q", line)
	}

	agent := line[3:endAgent]
	description := strings.TrimSpace(line[endAgent+2:])
	file := ""
	lineNumber := 0

	if strings.HasSuffix(description, ")") {
		locationStart := strings.LastIndex(description, " (")
		if locationStart >= 0 {
			location := strings.TrimSuffix(description[locationStart+2:], ")")
			description = strings.TrimSpace(description[:locationStart])
			file, lineNumber = parseLocation(location)
		}
	}

	return Issue{Agent: agent, Severity: severity, Description: description, File: file, Line: lineNumber}, nil
}

func parseLocation(location string) (string, int) {
	colon := strings.LastIndex(location, ":")
	if colon < 0 {
		return location, 0
	}

	lineNumber, err := strconv.Atoi(location[colon+1:])
	if err != nil || lineNumber <= 0 {
		return location, 0
	}

	return location[:colon], lineNumber
}
