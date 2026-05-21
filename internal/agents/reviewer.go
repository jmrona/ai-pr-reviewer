package agents

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

type ReviewTrace struct {
	Model           string
	ReasoningEffort string
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
	skills          map[string]string
	includePrompts  bool
}

func NewOpenAIReviewer(apiKey, model, reasoningEffort string, skills map[string]string, includePrompts bool) *OpenAIReviewer {
	return &OpenAIReviewer{
		client:          openai.NewClient(option.WithAPIKey(apiKey)),
		model:           model,
		reasoningEffort: reasoningEffort,
		skills:          skills,
		includePrompts:  includePrompts,
	}
}

func (r *OpenAIReviewer) Review(ctx context.Context, ticketContext, diff string, diffTruncated bool) (ReviewOutcome, error) {
	debate := make([]AgentMessage, 0, 3)
	trace := ReviewTrace{Model: r.model, ReasoningEffort: r.reasoningEffort, IncludePrompts: r.includePrompts, AgentMessages: make([]AgentTraceMessage, 0, 4)}

	pragmatist, pragmatistTrace, err := r.runAgent(ctx, "Pragmatist", "pragmatist", ticketContext, diff, nil, 0.3)
	if err != nil {
		return ReviewOutcome{}, err
	}
	debate = append(debate, AgentMessage{Agent: "Pragmatist", Content: pragmatist})
	trace.AgentMessages = append(trace.AgentMessages, pragmatistTrace)

	architect, architectTrace, err := r.runAgent(ctx, "Architect", "architect", ticketContext, diff, debate, 0.3)
	if err != nil {
		return ReviewOutcome{}, err
	}
	debate = append(debate, AgentMessage{Agent: "Architect", Content: architect})
	trace.AgentMessages = append(trace.AgentMessages, architectTrace)

	designer, designerTrace, err := r.runAgent(ctx, "Designer", "designer", ticketContext, diff, debate, 0.3)
	if err != nil {
		return ReviewOutcome{}, err
	}
	debate = append(debate, AgentMessage{Agent: "Designer", Content: designer})
	trace.AgentMessages = append(trace.AgentMessages, designerTrace)

	moderator, moderatorTrace, err := r.runAgent(ctx, "Moderator", "moderator", ticketContext, diff, debate, 0.1)
	if err != nil {
		return ReviewOutcome{}, err
	}
	trace.AgentMessages = append(trace.AgentMessages, moderatorTrace)
	trace.ModeratorOutput = moderator

	result, err := ParseModeratorOutput(moderator)
	if err != nil {
		return ReviewOutcome{Trace: trace}, fmt.Errorf("parsing moderator output: %w", err)
	}
	result.AgentDebate = debate
	result.DiffTruncated = diffTruncated

	return ReviewOutcome{Result: result, Trace: trace}, nil
}

func (r *OpenAIReviewer) runAgent(ctx context.Context, agent, role, ticketContext, diff string, previous []AgentMessage, temperature float32) (string, AgentTraceMessage, error) {
	systemPrompt, err := ComposeSystemPrompt(r.skills, role)
	if err != nil {
		return "", AgentTraceMessage{}, err
	}
	userPrompt := buildUserPrompt(ticketContext, diff, previous)

	resp, err := r.client.Chat.Completions.New(ctx, buildChatCompletionParams(r.model, r.reasoningEffort, systemPrompt, userPrompt, float64(temperature)))
	if err != nil {
		return "", AgentTraceMessage{}, wrapAgentRunError(agent, err)
	}
	if len(resp.Choices) == 0 {
		return "", AgentTraceMessage{}, emptyAgentResponseError(agent)
	}

	output := resp.Choices[0].Message.Content
	return output, buildAgentTraceMessage(agent, systemPrompt, userPrompt, output, r.includePrompts), nil
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

func buildUserPrompt(ticketContext, diff string, previous []AgentMessage) string {
	var b strings.Builder
	b.WriteString("=== JIRA TICKET ===\n")
	b.WriteString(ticketContext)
	b.WriteString("\n\n=== MR DIFF ===\n")
	b.WriteString(diff)

	if len(previous) > 0 {
		b.WriteString("\n\n=== PREVIOUS AGENT ANALYSIS ===\n")
		for _, msg := range previous {
			b.WriteString(msg.Agent)
			b.WriteString(":\n")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")
		}
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
