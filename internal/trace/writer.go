package trace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/example/ai-pr-reviewer/internal/agents"
)

const redacted = "[REDACTED]"

var (
	slackHookPattern     = regexp.MustCompile(`https://hooks\.slack\.com/\S+`)
	authorizationPattern = regexp.MustCompile(`(?m)Authorization:[^\r\n]*`)
	bearerPattern        = regexp.MustCompile(`Bearer [A-Za-z0-9._~+/=-]+`)
)

type Writer struct {
	Enabled        bool
	Dir            string
	IncludePrompts bool
	Redactions     []string
}

type TraceInput struct {
	IssueKey              string
	MRURL                 string
	TicketURL             string
	AdditionalInstruction string
	TicketContext         string
	Diff                  string
	DiffTruncated         bool
	ReviewOutcome         agents.ReviewOutcome
	SlackMessage          string
	CreatedAt             time.Time
}

func NewWriter(enabled bool, dir string, includePrompts bool, redactions []string) Writer {
	return Writer{Enabled: enabled, Dir: dir, IncludePrompts: includePrompts, Redactions: redactions}
}

func (w Writer) Write(ctx context.Context, input TraceInput) (string, error) {
	if !w.Enabled {
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if err := os.MkdirAll(w.Dir, 0o755); err != nil {
		return "", fmt.Errorf("creating trace directory: %w", err)
	}

	createdAt := input.CreatedAt.UTC()
	path := filepath.Join(w.Dir, fmt.Sprintf("%s-%s.md", safeTicketKey(input.IssueKey), createdAt.Format("20060102T150405Z")))
	content := w.redact(renderMarkdown(input, createdAt, w.IncludePrompts))

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("writing trace file: %w", err)
	}

	return path, nil
}

func renderMarkdown(input TraceInput, createdAt time.Time, includePrompts bool) string {
	var b strings.Builder
	outcome := input.ReviewOutcome

	b.WriteString("# Review Trace: ")
	b.WriteString(input.IssueKey)
	b.WriteString("\n\n")

	b.WriteString("## Metadata\n\n")
	writeField(&b, "Ticket", input.IssueKey)
	writeField(&b, "MR URL", input.MRURL)
	writeField(&b, "Ticket URL", input.TicketURL)
	writeField(&b, "Additional instruction", input.AdditionalInstruction)
	writeField(&b, "Timestamp", createdAt.Format(time.RFC3339))
	writeField(&b, "Model", outcome.Trace.Model)
	writeField(&b, "Reasoning effort", outcome.Trace.ReasoningEffort)
	writeField(&b, "Review rounds", fmt.Sprintf("%d", outcome.Trace.ReviewRounds))
	writeField(&b, "Diff truncated", fmt.Sprintf("%t", input.DiffTruncated))
	if includePrompts {
		writeField(&b, "Prompt capture", "enabled")
	} else {
		writeField(&b, "Prompt capture", "disabled")
	}
	b.WriteString("\n")

	writeSection(&b, "Ticket Context", input.TicketContext)
	writeSection(&b, "MR Diff", input.Diff)
	writePrompts(&b, outcome.Trace.AgentMessages, includePrompts)
	writeAgentOutputs(&b, outcome.Trace.AgentMessages)
	writeSection(&b, "Moderator Output", outcome.Trace.ModeratorOutput)
	writeParsedReviewResult(&b, outcome.Result)
	writeSection(&b, "Final Slack Message", input.SlackMessage)

	return b.String()
}

func writeField(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

func writeSection(b *strings.Builder, title, content string) {
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n\n")
	if content != "" {
		b.WriteString(content)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writePrompts(b *strings.Builder, messages []agents.AgentTraceMessage, includePrompts bool) {
	b.WriteString("## Prompts\n\n")
	if !includePrompts {
		b.WriteString("Prompt capture disabled.\n\n")
		return
	}
	if !hasPromptContent(messages) {
		return
	}

	for _, message := range messages {
		if message.SystemPrompt == "" && message.UserPrompt == "" {
			continue
		}
		b.WriteString("### ")
		b.WriteString(message.Agent)
		b.WriteString("\n\n")
		writeTraceDuration(b, message.Duration)
		if message.SystemPrompt != "" {
			writeSubsection(b, "System Prompt", message.SystemPrompt)
		}
		if message.UserPrompt != "" {
			writeSubsection(b, "User Prompt", message.UserPrompt)
		}
	}
}

func writeSubsection(b *strings.Builder, title, content string) {
	b.WriteString("#### ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(content)
	b.WriteString("\n\n")
}

func writeAgentOutputs(b *strings.Builder, messages []agents.AgentTraceMessage) {
	b.WriteString("## Agent Outputs\n\n")
	for _, message := range messages {
		if message.Output == "" {
			continue
		}
		b.WriteString("### ")
		b.WriteString(message.Agent)
		b.WriteString("\n\n")
		writeTraceDuration(b, message.Duration)
		b.WriteString(message.Output)
		b.WriteString("\n\n")
	}
}

func writeTraceDuration(b *strings.Builder, duration time.Duration) {
	formatted := formatTraceDuration(duration)
	if formatted == "" {
		return
	}
	writeField(b, "Duration", formatted)
	b.WriteString("\n")
}

func formatTraceDuration(duration time.Duration) string {
	if duration == 0 {
		return ""
	}
	if duration < time.Millisecond && duration > -time.Millisecond {
		return "<1ms"
	}
	return duration.Round(time.Millisecond).String()
}

func writeParsedReviewResult(b *strings.Builder, result agents.ReviewResult) {
	b.WriteString("## Parsed Review Result\n\n")
	writeSubsection(b, "Ticket Coverage", result.TicketCoverage)
	writeIssues(b, "Blockers", result.Blockers)
	writeIssues(b, "Warnings", result.Warnings)
	writeIssues(b, "Suggestions", result.Suggestions)
	writeStrings(b, "Assumptions", result.Assumptions)
	writeSubsection(b, "Summary", result.Summary)
}

func writeIssues(b *strings.Builder, title string, issues []agents.Issue) {
	b.WriteString("#### ")
	b.WriteString(title)
	b.WriteString("\n\n")
	if len(issues) == 0 {
		b.WriteString("None\n\n")
		return
	}
	for _, issue := range issues {
		b.WriteString("- [")
		b.WriteString(issue.Agent)
		b.WriteString("] ")
		b.WriteString(issue.Description)
		if issue.File != "" {
			b.WriteString(" (")
			b.WriteString(issue.File)
			if issue.Line > 0 {
				b.WriteString(fmt.Sprintf(":%d", issue.Line))
			}
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeStrings(b *strings.Builder, title string, values []string) {
	b.WriteString("#### ")
	b.WriteString(title)
	b.WriteString("\n\n")
	if len(values) == 0 {
		b.WriteString("None\n\n")
		return
	}
	for _, value := range values {
		b.WriteString("- ")
		b.WriteString(value)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func hasPromptContent(messages []agents.AgentTraceMessage) bool {
	for _, message := range messages {
		if message.SystemPrompt != "" || message.UserPrompt != "" {
			return true
		}
	}
	return false
}

func safeTicketKey(issueKey string) string {
	if issueKey == "" {
		return "review"
	}

	var b strings.Builder
	for _, r := range issueKey {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}

	return b.String()
}

func (w Writer) redact(content string) string {
	for _, value := range w.Redactions {
		if value == "" {
			continue
		}
		content = strings.ReplaceAll(content, value, redacted)
	}
	content = slackHookPattern.ReplaceAllString(content, redacted)
	content = authorizationPattern.ReplaceAllString(content, redacted)
	content = bearerPattern.ReplaceAllString(content, redacted)
	return content
}
