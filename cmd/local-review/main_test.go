package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDotEnvKeepsExistingEnvironmentValues(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("SLACK_SIGNING_SECRET", "from-env")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("\n# comment\nPORT=7777\nSLACK_SIGNING_SECRET=from-file\nGITLAB_TOKEN=gitlab\n"), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}

	env, err := loadDotEnv(dir)
	if err != nil {
		t.Fatalf("loading .env: %v", err)
	}

	if env["PORT"] != "9999" {
		t.Fatalf("PORT = %q, want existing process value", env["PORT"])
	}
	if env["SLACK_SIGNING_SECRET"] != "from-env" {
		t.Fatalf("SLACK_SIGNING_SECRET = %q, want existing process value", env["SLACK_SIGNING_SECRET"])
	}
	if env["GITLAB_TOKEN"] != "gitlab" {
		t.Fatalf("GITLAB_TOKEN = %q, want .env value", env["GITLAB_TOKEN"])
	}
}

func TestLoadDotEnvDefaultsPortForLocalReview(t *testing.T) {
	t.Setenv("SLACK_SIGNING_SECRET", "secret")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SLACK_SIGNING_SECRET=file-secret\n"), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}

	env, err := loadDotEnv(dir)
	if err != nil {
		t.Fatalf("loading .env: %v", err)
	}

	if env["PORT"] != "8888" {
		t.Fatalf("PORT = %q, want local-review default", env["PORT"])
	}
}

func TestRequireLocalConfigFailsClearlyWithoutSlackSigningSecret(t *testing.T) {
	err := requireLocalConfig(map[string]string{"PORT": "8888"})
	if err == nil {
		t.Fatal("expected missing config error")
	}

	if !strings.Contains(err.Error(), "SLACK_SIGNING_SECRET") {
		t.Fatalf("error = %q, want missing SLACK_SIGNING_SECRET", err.Error())
	}
}

func TestSignSlackRequestUsesSlackVersionedHMACBaseString(t *testing.T) {
	signature := signSlackRequest("secret", "123", []byte("text=hello"))

	if signature != "v0=bb9823c2da1da1399f117abbdc76dc518d1d9b591f5c19cd976d430f40759a8f" {
		t.Fatalf("signature = %q", signature)
	}
}

func TestExtractParsedReviewResultReturnsOnlyParsedSection(t *testing.T) {
	trace := "before\n## Parsed Review Result\n\nResult line 1\nResult line 2\n\n## Final Slack Message\nafter\n"

	result, err := extractParsedReviewResult([]byte(trace))
	if err != nil {
		t.Fatalf("extracting parsed result: %v", err)
	}

	if result != "Result line 1\nResult line 2" {
		t.Fatalf("result = %q", result)
	}
}

func TestSelectNewestMatchingTraceUsesSubmissionSnapshotAndExactURLs(t *testing.T) {
	dir := t.TempDir()
	mrURL := "https://gitlab.example.com/group/project/-/merge_requests/7"
	ticketURL := "https://jira.example.com/browse/PROJ-7"
	submissionTime := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	before := map[string]time.Time{}

	oldMatch := filepath.Join(dir, "old.md")
	if err := os.WriteFile(oldMatch, []byte("MR URL: "+mrURL+"\nTicket URL: "+ticketURL), 0o600); err != nil {
		t.Fatalf("writing old trace: %v", err)
	}
	oldTime := submissionTime.Add(-time.Minute)
	if err := os.Chtimes(oldMatch, oldTime, oldTime); err != nil {
		t.Fatalf("dating old trace: %v", err)
	}
	before[oldMatch] = oldTime

	newNonMatch := filepath.Join(dir, "new-non-match.md")
	if err := os.WriteFile(newNonMatch, []byte("MR URL: "+mrURL+"\nTicket URL: https://jira.example.com/browse/OTHER-1"), 0o600); err != nil {
		t.Fatalf("writing non-matching trace: %v", err)
	}
	if err := os.Chtimes(newNonMatch, submissionTime.Add(time.Minute), submissionTime.Add(time.Minute)); err != nil {
		t.Fatalf("dating non-matching trace: %v", err)
	}

	newMatch := filepath.Join(dir, "new-match.md")
	if err := os.WriteFile(newMatch, []byte("prefix\nMR URL: "+mrURL+"\nTicket URL: "+ticketURL), 0o600); err != nil {
		t.Fatalf("writing matching trace: %v", err)
	}
	if err := os.Chtimes(newMatch, submissionTime.Add(2*time.Minute), submissionTime.Add(2*time.Minute)); err != nil {
		t.Fatalf("dating matching trace: %v", err)
	}

	selected, err := selectNewestMatchingTrace(dir, before, submissionTime, mrURL, ticketURL)
	if err != nil {
		t.Fatalf("selecting trace: %v", err)
	}

	if selected != newMatch {
		t.Fatalf("selected = %q, want %q", selected, newMatch)
	}
}

func TestSelectNewestMatchingTraceRejectsSubstringURLMatches(t *testing.T) {
	dir := t.TempDir()
	mrURL := "https://gitlab.example.com/group/project/-/merge_requests/7"
	ticketURL := "https://jira.example.com/browse/PROJ-7"
	submissionTime := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	substringMatch := filepath.Join(dir, "substring.md")
	content := "https://gitlab.example.com/group/project/-/merge_requests/77\nhttps://jira.example.com/browse/PROJ-77"
	if err := os.WriteFile(substringMatch, []byte(content), 0o600); err != nil {
		t.Fatalf("writing substring trace: %v", err)
	}
	if err := os.Chtimes(substringMatch, submissionTime.Add(time.Minute), submissionTime.Add(time.Minute)); err != nil {
		t.Fatalf("dating substring trace: %v", err)
	}

	_, err := selectNewestMatchingTrace(dir, map[string]time.Time{}, submissionTime, mrURL, ticketURL)
	if err == nil {
		t.Fatal("selectNewestMatchingTrace() error = nil, want no matching trace")
	}
}

func TestSelectNewestMatchingTraceRequiresModificationAfterSubmission(t *testing.T) {
	dir := t.TempDir()
	mrURL := "https://gitlab.example.com/group/project/-/merge_requests/7"
	ticketURL := "https://jira.example.com/browse/PROJ-7"
	submissionTime := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	tooEarly := filepath.Join(dir, "too-early.md")
	if err := os.WriteFile(tooEarly, []byte(mrURL+"\n"+ticketURL), 0o600); err != nil {
		t.Fatalf("writing early trace: %v", err)
	}
	if err := os.Chtimes(tooEarly, submissionTime.Add(-time.Second), submissionTime.Add(-time.Second)); err != nil {
		t.Fatalf("dating early trace: %v", err)
	}

	_, err := selectNewestMatchingTrace(dir, map[string]time.Time{}, submissionTime, mrURL, ticketURL)
	if err == nil {
		t.Fatal("selectNewestMatchingTrace() error = nil, want no matching trace")
	}
}
