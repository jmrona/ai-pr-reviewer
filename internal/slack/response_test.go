package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/ai-pr-reviewer/internal/agents"
)

func TestFormatReviewResultIncludesAssumptionsAndTruncation(t *testing.T) {
	message := FormatReviewResult(agents.ReviewResult{
		TicketCoverage: ":white_check_mark: All criteria covered",
		Blockers:       []agents.Issue{{Agent: "Architect", Description: "N+1 query", File: "user.go", Line: 47}},
		Assumptions:    []string{"Jira description is current"},
		Summary:        "Fix blocker before merge.",
		DiffTruncated:  true,
	}, "PROJ-141", "https://gitlab.example.com/platform/application/-/merge_requests/1")

	for _, want := range []string{"AI PR Review", "PROJ-141", "N+1 query (user.go:47)", "Jira description is current", "diffs were truncated"} {
		if !strings.Contains(message, want) {
			t.Fatalf("FormatReviewResult() missing %q in %q", want, message)
		}
	}
}

func TestFormatReviewResultOmitsIssueKeySegmentWhenIssueKeyIsEmpty(t *testing.T) {
	message := FormatReviewResult(agents.ReviewResult{
		TicketCoverage: ":white_check_mark: All criteria covered",
		Summary:        "Looks good.",
	}, "", "https://gitlab.example.com/platform/application/-/merge_requests/1")

	if strings.Contains(message, "``") {
		t.Fatalf("FormatReviewResult() included empty issue key segment: %q", message)
	}
	if !strings.Contains(message, "<https://gitlab.example.com/platform/application/-/merge_requests/1|View MR>") {
		t.Fatalf("FormatReviewResult() missing MR link: %q", message)
	}
}

func TestPosterPostsResponseURLPayload(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	poster := NewPoster(server.Client())
	if err := poster.Post(context.Background(), server.URL, "hello"); err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if !strings.Contains(gotBody, "hello") {
		t.Fatalf("posted body = %q, want hello", gotBody)
	}
}

func TestPosterPostsProgressPayloadWithStableDiscriminator(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	poster := NewPoster(server.Client())
	if err := poster.PostProgress(context.Background(), server.URL, "Fetching merge request context..."); err != nil {
		t.Fatalf("PostProgress() error = %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("progress body is not JSON: %v", err)
	}
	if payload["type"] != "progress" || payload["message"] != "Fetching merge request context..." {
		t.Fatalf("progress payload = %#v, want type=progress and message", payload)
	}
	if _, ok := payload["text"]; ok {
		t.Fatalf("progress payload = %#v, want no Slack final text field", payload)
	}
}
