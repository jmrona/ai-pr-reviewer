package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	baseURL *url.URL
	email   string
	token   string
	http    *http.Client
}

type Issue struct {
	Key    string `json:"key"`
	Fields Fields `json:"fields"`
}

type Fields struct {
	Summary     string  `json:"summary"`
	Description ADFNode `json:"description"`
	Status      Named   `json:"status"`
	IssueType   Named   `json:"issuetype"`
	Priority    Named   `json:"priority"`
}

type Named struct {
	Name string `json:"name"`
}

type ADFNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []ADFNode `json:"content"`
}

func NewClient(baseURL, email, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing Jira base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("parsing Jira base URL: missing scheme or host")
	}
	return &Client{baseURL: parsed, email: email, token: token, http: httpClient}, nil
}

func ParseTicketURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing Jira ticket URL: %w", err)
	}

	segments := pathSegments(parsed.Path)
	for i := 0; i+1 < len(segments); i++ {
		if segments[i] == "browse" {
			issueKey := segments[i+1]
			if !validIssueKey(issueKey) {
				return "", fmt.Errorf("parsing Jira ticket URL: invalid issue key")
			}
			return issueKey, nil
		}
	}

	return "", fmt.Errorf("parsing Jira ticket URL: expected /browse/{issue-key}")
}

func (c *Client) ClassifyTicketURL(rawURL string) (string, bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false, fmt.Errorf("parsing Jira ticket URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", false, nil
	}
	if !strings.EqualFold(parsed.Hostname(), c.baseURL.Hostname()) {
		return "", false, nil
	}

	issueKey, err := ParseTicketURL(rawURL)
	if err != nil {
		return "", true, err
	}
	return issueKey, true, nil
}

func (c *Client) GetIssue(ctx context.Context, issueKey string) (*Issue, error) {
	apiURL := c.baseURL.ResolveReference(&url.URL{Path: "/rest/api/3/issue/" + issueKey})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating Jira request: %w", err)
	}
	req.SetBasicAuth(c.email, c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching Jira issue: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetching Jira issue: status %d: %s", resp.StatusCode, safeSnippet(resp.Body))
	}

	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("decoding Jira issue: %w", err)
	}
	return &issue, nil
}

func (c *Client) FetchTicketContext(ctx context.Context, issueKey string) (string, error) {
	issue, err := c.GetIssue(ctx, issueKey)
	if err != nil {
		return "", err
	}
	return FormatIssue(issue), nil
}

func FormatIssue(issue *Issue) string {
	if issue == nil {
		return "No Jira issue returned."
	}

	var b strings.Builder
	b.WriteString("Key: ")
	b.WriteString(issue.Key)
	b.WriteString("\nSummary: ")
	b.WriteString(issue.Fields.Summary)
	b.WriteString("\nType: ")
	b.WriteString(issue.Fields.IssueType.Name)
	b.WriteString("\nStatus: ")
	b.WriteString(issue.Fields.Status.Name)
	b.WriteString("\nPriority: ")
	b.WriteString(issue.Fields.Priority.Name)
	b.WriteString("\nDescription:\n")
	b.WriteString(strings.TrimSpace(ExtractADFText(issue.Fields.Description)))
	return strings.TrimSpace(b.String())
}

func ExtractADFText(node ADFNode) string {
	var b strings.Builder
	writeADF(&b, node, 0)
	return strings.TrimSpace(b.String())
}

func writeADF(b *strings.Builder, node ADFNode, depth int) {
	switch node.Type {
	case "text":
		b.WriteString(node.Text)
	case "paragraph", "heading":
		writeChildren(b, node.Content, depth)
		b.WriteString("\n")
	case "bulletList", "orderedList":
		writeChildren(b, node.Content, depth)
	case "listItem":
		b.WriteString(strings.Repeat("  ", depth))
		b.WriteString("- ")
		writeChildren(b, node.Content, depth+1)
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
	default:
		writeChildren(b, node.Content, depth)
	}
}

func writeChildren(b *strings.Builder, children []ADFNode, depth int) {
	for _, child := range children {
		writeADF(b, child, depth)
	}
}

func pathSegments(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

func validIssueKey(issueKey string) bool {
	hyphen := strings.Index(issueKey, "-")
	return hyphen > 0 && hyphen < len(issueKey)-1
}

func safeSnippet(r io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(r, 500))
	if err != nil {
		return "unable to read response body"
	}
	return string(data)
}
