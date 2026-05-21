package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const diffLimit = 3000

type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

type MRChanges struct {
	Changes []Change `json:"changes"`
}

type Change struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	Diff    string `json:"diff"`
}

func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing GitLab base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("parsing GitLab base URL: missing scheme or host")
	}
	return &Client{baseURL: parsed, token: token, http: httpClient}, nil
}

func ParseMRURL(rawURL string) (string, int, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, fmt.Errorf("parsing GitLab MR URL: %w", err)
	}

	segments := pathSegments(parsed.Path)
	for i := 0; i+2 < len(segments); i++ {
		if segments[i] == "-" && segments[i+1] == "merge_requests" {
			if i == 0 {
				return "", 0, fmt.Errorf("parsing GitLab MR URL: missing project path")
			}

			mrIID, err := strconv.Atoi(segments[i+2])
			if err != nil || mrIID <= 0 {
				return "", 0, fmt.Errorf("parsing GitLab MR URL: invalid merge request IID")
			}

			return strings.Join(segments[:i], "/"), mrIID, nil
		}
	}

	return "", 0, fmt.Errorf("parsing GitLab MR URL: expected /-/merge_requests/{iid}")
}

func (c *Client) ClassifyMRURL(rawURL string) (string, int, bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, false, fmt.Errorf("parsing GitLab MR URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", 0, false, nil
	}
	if !strings.EqualFold(parsed.Hostname(), c.baseURL.Hostname()) {
		return "", 0, false, nil
	}

	projectPath, mrIID, err := ParseMRURL(rawURL)
	if err != nil {
		return "", 0, true, err
	}
	return projectPath, mrIID, true, nil
}

func (c *Client) GetMRChanges(ctx context.Context, projectPath string, mrIID int) (*MRChanges, error) {
	apiURL := strings.TrimRight(c.baseURL.String(), "/") + "/api/v4/projects/" + url.PathEscape(projectPath) + "/merge_requests/" + strconv.Itoa(mrIID) + "/changes"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GitLab request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching GitLab MR changes: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetching GitLab MR changes: status %d: %s", resp.StatusCode, safeSnippet(resp.Body))
	}

	var changes MRChanges
	if err := json.NewDecoder(resp.Body).Decode(&changes); err != nil {
		return nil, fmt.Errorf("decoding GitLab MR changes: %w", err)
	}
	return &changes, nil
}

func (c *Client) FetchChangeContext(ctx context.Context, projectPath string, mrIID int) (string, bool, error) {
	changes, err := c.GetMRChanges(ctx, projectPath, mrIID)
	if err != nil {
		return "", false, err
	}
	formatted, truncated := FormatDiff(changes)
	return formatted, truncated, nil
}

func FormatDiff(changes *MRChanges) (string, bool) {
	if changes == nil || len(changes.Changes) == 0 {
		return "No file changes returned by GitLab.", false
	}

	var b strings.Builder
	truncated := false
	for _, change := range changes.Changes {
		path := change.NewPath
		if path == "" {
			path = change.OldPath
		}

		diff := change.Diff
		fileTruncated := false
		if len(diff) > diffLimit {
			diff = diff[:diffLimit]
			truncated = true
			fileTruncated = true
		}

		b.WriteString("--- FILE: ")
		b.WriteString(path)
		b.WriteString(" ---\n")
		b.WriteString(diff)
		if fileTruncated {
			b.WriteString("\n[diff truncated at 3000 characters]")
		}
		b.WriteString("\n\n")
	}

	return strings.TrimSpace(b.String()), truncated
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

func safeSnippet(r io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(r, 500))
	if err != nil {
		return "unable to read response body"
	}
	return string(data)
}
