package agents

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	openai "github.com/sashabaranov/go-openai"
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

type OpenAIReviewer struct {
	client *openai.Client
	model  string
	skills map[string]string
}

func NewOpenAIReviewer(apiKey, model string, skills map[string]string) *OpenAIReviewer {
	return &OpenAIReviewer{
		client: openai.NewClient(apiKey),
		model:  model,
		skills: skills,
	}
}

func (r *OpenAIReviewer) Review(ctx context.Context, ticketContext, diff string, diffTruncated bool) (ReviewResult, error) {
	debate := make([]AgentMessage, 0, 3)

	pragmatist, err := r.runAgent(ctx, "Pragmatist", "pragmatist", ticketContext, diff, nil, 0.3)
	if err != nil {
		return ReviewResult{}, err
	}
	debate = append(debate, AgentMessage{Agent: "Pragmatist", Content: pragmatist})

	architect, err := r.runAgent(ctx, "Architect", "architect", ticketContext, diff, debate, 0.3)
	if err != nil {
		return ReviewResult{}, err
	}
	debate = append(debate, AgentMessage{Agent: "Architect", Content: architect})

	designer, err := r.runAgent(ctx, "Designer", "designer", ticketContext, diff, debate, 0.3)
	if err != nil {
		return ReviewResult{}, err
	}
	debate = append(debate, AgentMessage{Agent: "Designer", Content: designer})

	moderator, err := r.runAgent(ctx, "Moderator", "moderator", ticketContext, diff, debate, 0.1)
	if err != nil {
		return ReviewResult{}, err
	}

	result, err := ParseModeratorOutput(moderator)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("parsing moderator output: %w", err)
	}
	result.AgentDebate = debate
	result.DiffTruncated = diffTruncated

	return result, nil
}

func (r *OpenAIReviewer) runAgent(ctx context.Context, agent, role, ticketContext, diff string, previous []AgentMessage, temperature float32) (string, error) {
	systemPrompt, err := ComposeSystemPrompt(r.skills, role)
	if err != nil {
		return "", err
	}

	resp, err := r.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       r.model,
		Temperature: temperature,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: buildUserPrompt(ticketContext, diff, previous)},
		},
	})
	if err != nil {
		return "", fmt.Errorf("running %s agent: %w", agent, err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("running %s agent: empty response", agent)
	}

	return resp.Choices[0].Message.Content, nil
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
		switch line {
		case "TICKET_COVERAGE:", "BLOCKERS:", "WARNINGS:", "SUGGESTIONS:", "ASSUMPTIONS:", "SUMMARY:":
			current = strings.TrimSuffix(line, ":")
			sections[current] = []string{}
		default:
			if current != "" && line != "" {
				sections[current] = append(sections[current], line)
			}
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

func parseTextSection(lines []string) []string {
	if len(lines) == 1 && lines[0] == "None" {
		return nil
	}

	values := make([]string, 0, len(lines))
	for _, line := range lines {
		values = append(values, strings.TrimPrefix(line, "- "))
	}
	return values
}

func parseIssueSection(severity string, lines []string) ([]Issue, error) {
	if len(lines) == 1 && lines[0] == "None" {
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
