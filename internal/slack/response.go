package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/ai-pr-reviewer/internal/agents"
)

type Poster struct {
	http *http.Client
}

func NewPoster(httpClient *http.Client) *Poster {
	return &Poster{http: httpClient}
}

func (p *Poster) Post(ctx context.Context, responseURL, text string) error {
	body, err := json.Marshal(map[string]string{
		"response_type": "in_channel",
		"text":          text,
	})
	if err != nil {
		return fmt.Errorf("encoding Slack response: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating Slack response request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("posting Slack response: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("posting Slack response: status %d", resp.StatusCode)
	}

	return nil
}

func FormatReviewResult(result agents.ReviewResult, issueKey, mrURL string) string {
	var b strings.Builder
	b.WriteString("*:robot_face: AI PR Review*")
	if issueKey != "" {
		b.WriteString(" | `")
		b.WriteString(issueKey)
		b.WriteString("`")
	}
	b.WriteString(" | <")
	b.WriteString(mrURL)
	b.WriteString("|View MR>\n\n")

	b.WriteString("*Ticket Coverage:* ")
	b.WriteString(result.TicketCoverage)
	b.WriteString("\n\n")

	writeIssueSection(&b, ":x:", "Blockers", result.Blockers)
	writeIssueSection(&b, ":warning:", "Warnings", result.Warnings)
	writeIssueSection(&b, ":bulb:", "Suggestions", result.Suggestions)

	if len(result.Assumptions) > 0 {
		b.WriteString("*Assumptions:*\n")
		for _, assumption := range result.Assumptions {
			b.WriteString("- ")
			b.WriteString(assumption)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if result.DiffTruncated {
		b.WriteString("_Note: one or more file diffs were truncated before review._\n\n")
	}

	b.WriteString("_Overall: ")
	b.WriteString(result.Summary)
	b.WriteString("_")

	return b.String()
}

func FormatError(message string) string {
	return ":warning: AI PR review failed: " + message
}

func writeIssueSection(b *strings.Builder, icon, title string, issues []agents.Issue) {
	b.WriteString("*")
	b.WriteString(icon)
	b.WriteString(" ")
	b.WriteString(title)
	b.WriteString(" (")
	b.WriteString(strconv.Itoa(len(issues)))
	b.WriteString("):*\n")

	if len(issues) == 0 {
		b.WriteString("None\n\n")
		return
	}

	for _, issue := range issues {
		b.WriteString("• [")
		b.WriteString(issue.Agent)
		b.WriteString("] ")
		b.WriteString(issue.Description)
		if issue.File != "" {
			b.WriteString(" (")
			b.WriteString(issue.File)
			if issue.Line > 0 {
				b.WriteString(":")
				b.WriteString(strconv.Itoa(issue.Line))
			}
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}
