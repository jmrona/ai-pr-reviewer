package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
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

func TestPromptRequiredWritesStyledPromptAndReturnsValue(t *testing.T) {
	var stderr bytes.Buffer
	stdin := bufio.NewReader(strings.NewReader("answer\n"))

	value, err := promptRequired(&stderr, stdin, "GitLab MR URL: ")
	if err != nil {
		t.Fatalf("promptRequired() error = %v", err)
	}

	if value != "answer" {
		t.Fatalf("value = %q, want answer", value)
	}
	if !strings.Contains(stderr.String(), "\x1b[") || !strings.Contains(stderr.String(), "GitLab MR URL") {
		t.Fatalf("stderr prompt = %q, want styled label", stderr.String())
	}
}

func TestPromptOptionalWithDefaultWritesStyledPromptAndReturnsDefault(t *testing.T) {
	var stderr bytes.Buffer
	stdin := bufio.NewReader(strings.NewReader("\n"))

	value, err := promptOptionalWithDefault(&stderr, stdin, "Reasoning effort override", " high ")
	if err != nil {
		t.Fatalf("promptOptionalWithDefault() error = %v", err)
	}

	if value != "high" {
		t.Fatalf("value = %q, want high", value)
	}
	prompt := stderr.String()
	if !strings.Contains(prompt, "\x1b[") || !strings.Contains(prompt, "Reasoning effort override") || !strings.Contains(prompt, "[high]") {
		t.Fatalf("stderr prompt = %q, want styled prompt with default", prompt)
	}
}

func TestPromptReviewInputsUsesLocalDefaultsForBlankModelAndEffort(t *testing.T) {
	stdin := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"https://gitlab.example.com/group/project/-/merge_requests/7",
		"",
		"",
		"",
		"",
	}, "\n")))
	env := map[string]string{
		"OPENAI_MODEL":            "gpt-5.5",
		"OPENAI_REASONING_EFFORT": "high",
	}

	input, err := promptReviewInputs(io.Discard, stdin, env)
	if err != nil {
		t.Fatalf("prompting review inputs: %v", err)
	}

	if input.MRURL != "https://gitlab.example.com/group/project/-/merge_requests/7" {
		t.Fatalf("MRURL = %q", input.MRURL)
	}
	if input.TicketURL != "" {
		t.Fatalf("TicketURL = %q, want empty", input.TicketURL)
	}
	if input.Model != "gpt-5.5" {
		t.Fatalf("Model = %q, want local default", input.Model)
	}
	if input.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %q, want local default", input.ReasoningEffort)
	}
	if input.AdditionalInstruction != "" {
		t.Fatalf("AdditionalInstruction = %q, want empty", input.AdditionalInstruction)
	}
}

func TestPromptReviewInputsNormalisesDefaultReasoningEffortBeforeSubmission(t *testing.T) {
	stdin := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"https://gitlab.example.com/group/project/-/merge_requests/7",
		"",
		"",
		"",
		"",
	}, "\n")))
	env := map[string]string{
		"OPENAI_REASONING_EFFORT": " XHIGH ",
	}

	input, err := promptReviewInputs(io.Discard, stdin, env)
	if err != nil {
		t.Fatalf("prompting review inputs: %v", err)
	}

	if input.ReasoningEffort != "xhigh" {
		t.Fatalf("ReasoningEffort = %q, want normalised xhigh", input.ReasoningEffort)
	}
}

func TestPromptReviewInputsRejectsInvalidReasoningEffortBeforeSubmission(t *testing.T) {
	stdin := bufio.NewReader(strings.NewReader(strings.Join([]string{
		"https://gitlab.example.com/group/project/-/merge_requests/7",
		"",
		"",
		"extreme",
		"",
	}, "\n")))

	_, err := promptReviewInputs(io.Discard, stdin, map[string]string{})
	if err == nil {
		t.Fatal("promptReviewInputs() error = nil, want invalid reasoning effort error")
	}
	if !strings.Contains(err.Error(), "reasoning effort") {
		t.Fatalf("error = %q, want reasoning effort validation", err.Error())
	}
}

func TestSubmitReviewIncludesOptionalFormFieldsOnlyWhenNonEmpty(t *testing.T) {
	requestBody := submitReviewToTestServer(t, reviewInput{
		MRURL:                 "https://gitlab.example.com/group/project/-/merge_requests/7",
		TicketURL:             "https://jira.example.com/browse/PROJ-7",
		Model:                 "gpt-5.5",
		ReasoningEffort:       "xhigh",
		AdditionalInstruction: "Focus on security regressions.",
	})

	values, err := url.ParseQuery(requestBody)
	if err != nil {
		t.Fatalf("parsing request body: %v", err)
	}

	wantText := "https://gitlab.example.com/group/project/-/merge_requests/7 https://jira.example.com/browse/PROJ-7"
	if values.Get("text") != wantText {
		t.Fatalf("text = %q, want %q", values.Get("text"), wantText)
	}
	if values.Get("model") != "gpt-5.5" {
		t.Fatalf("model = %q", values.Get("model"))
	}
	if values.Get("reasoning_effort") != "xhigh" {
		t.Fatalf("reasoning_effort = %q", values.Get("reasoning_effort"))
	}
	if values.Get("additional_instruction") != "Focus on security regressions." {
		t.Fatalf("additional_instruction = %q", values.Get("additional_instruction"))
	}
}

func TestSubmitReviewOmitsOptionalFieldsAndUsesMROnlyTextWhenBlank(t *testing.T) {
	requestBody := submitReviewToTestServer(t, reviewInput{
		MRURL: "https://gitlab.example.com/group/project/-/merge_requests/7",
	})

	values, err := url.ParseQuery(requestBody)
	if err != nil {
		t.Fatalf("parsing request body: %v", err)
	}

	if values.Get("text") != "https://gitlab.example.com/group/project/-/merge_requests/7" {
		t.Fatalf("text = %q, want MR URL only", values.Get("text"))
	}
	for _, field := range []string{"model", "reasoning_effort", "additional_instruction"} {
		if _, ok := values[field]; ok {
			t.Fatalf("%s was submitted with values %v, want omitted", field, values[field])
		}
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

func TestFormatTracePathMessageShowsSelectedTrace(t *testing.T) {
	message := formatTracePathMessage(".review-traces/PROJ-123.md")

	if message != "Using review trace: .review-traces/PROJ-123.md" {
		t.Fatalf("message = %q", message)
	}
}

func TestFormatElapsedReviewTimeUsesMinutesAndSeconds(t *testing.T) {
	line := formatElapsedReviewTime(2*time.Minute + 13*time.Second + 900*time.Millisecond)

	if line != "Review completed in 2m13s" {
		t.Fatalf("line = %q, want elapsed review timing", line)
	}
}

func TestFormatFinalReviewOutputEndsWithReviewBlankLineElapsedLineAndFinalNewline(t *testing.T) {
	output := formatFinalReviewOutput("Rendered review\nwith details", "Review completed in 2m13s")

	want := "Rendered review\nwith details\n\nReview completed in 2m13s\n"
	if output != want {
		t.Fatalf("output = %q, want exact final stdout composition", output)
	}
}

func TestAppendElapsedReviewTimeToTraceWritesCleanMarkdownSpacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.md")
	if err := os.WriteFile(path, []byte("## Final Slack Message\nReview body"), 0o600); err != nil {
		t.Fatalf("writing trace: %v", err)
	}

	if err := appendElapsedReviewTimeToTrace(path, "Review completed in 2m13s"); err != nil {
		t.Fatalf("appending elapsed review timing: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading trace: %v", err)
	}
	want := "## Final Slack Message\nReview body\n\nReview completed in 2m13s\n"
	if string(content) != want {
		t.Fatalf("trace content = %q, want clean elapsed timing append", string(content))
	}
}

func TestTraceExtractionErrorMessageExplainsReusedServerTraceSettings(t *testing.T) {
	message := traceExtractionErrorMessage(".review-traces", false)

	for _, want := range []string{"extracting trace failed in .review-traces", "reused server", "REVIEW_TRACE_ENABLED", "REVIEW_TRACE_DIR"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message = %q, want %q", message, want)
		}
	}
}

func TestEnsureTraceRecordedAdditionalInstructionFailsWhenMissing(t *testing.T) {
	trace := []byte("## Metadata\n\nMR URL: mr\n")

	err := ensureTraceRecordedAdditionalInstruction(trace, "Ignore generated files.")
	if err == nil {
		t.Fatal("ensureTraceRecordedAdditionalInstruction() error = nil, want missing instruction error")
	}
	if !strings.Contains(err.Error(), "additional instruction was not recorded") {
		t.Fatalf("error = %q, want missing instruction diagnostic", err.Error())
	}
}

func TestEnsureTraceRecordedAdditionalInstructionPassesWhenPresent(t *testing.T) {
	trace := []byte("Additional instruction: Ignore generated files.\n")

	if err := ensureTraceRecordedAdditionalInstruction(trace, "Ignore generated files."); err != nil {
		t.Fatalf("ensureTraceRecordedAdditionalInstruction() error = %v", err)
	}
}

func TestRenderParsedReviewResultRemovesHeadingMarkersAndAppliesStyles(t *testing.T) {
	result := strings.Join([]string{
		"# Summary",
		"## Findings",
		"### Major",
		"#### Ticket Coverage",
		"Body line",
	}, "\n")

	rendered := renderParsedReviewResult(result)

	want := strings.Join([]string{
		"\x1b[1;35mSummary\x1b[0m",
		"\x1b[1;34mFindings\x1b[0m",
		"\x1b[1;36mMajor\x1b[0m",
		"\x1b[1;33mTicket Coverage\x1b[0m",
		"Body line",
	}, "\n")
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
}

func TestRenderParsedReviewResultKeepsNonHeadingLinesUnchanged(t *testing.T) {
	result := strings.Join([]string{
		"Intro line",
		"",
		"- #### not a heading because it is a list item",
		"No blocker found.",
	}, "\n")

	rendered := renderParsedReviewResult(result)

	if rendered != result {
		t.Fatalf("rendered = %q, want unchanged result", rendered)
	}
}

func TestRenderParsedReviewResultKeepsFencedCodeBlocksUnchanged(t *testing.T) {
	result := strings.Join([]string{
		"#### Blockers",
		"```",
		"# comment inside code",
		"#### also code",
		"```",
	}, "\n")

	rendered := renderParsedReviewResult(result)

	want := strings.Join([]string{
		"\x1b[1;33mBlockers\x1b[0m",
		"```",
		"# comment inside code",
		"#### also code",
		"```",
	}, "\n")
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
}

func TestRenderParsedReviewResultKeepsLongFencedCodeBlocksUnchanged(t *testing.T) {
	result := strings.Join([]string{
		"````",
		"```",
		"# still code",
		"````",
	}, "\n")

	rendered := renderParsedReviewResult(result)

	if rendered != result {
		t.Fatalf("rendered = %q, want unchanged result", rendered)
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

func TestSelectNewestMatchingTraceMatchesOnlyMROnlyTraceWhenTicketURLIsBlank(t *testing.T) {
	dir := t.TempDir()
	mrURL := "https://gitlab.example.com/group/project/-/merge_requests/7"
	submissionTime := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	newerWithTicket := filepath.Join(dir, "newer-with-ticket.md")
	if err := os.WriteFile(newerWithTicket, []byte("MR URL: "+mrURL+"\nTicket URL: https://jira.example.com/browse/OTHER-1"), 0o600); err != nil {
		t.Fatalf("writing newer ticket trace: %v", err)
	}
	if err := os.Chtimes(newerWithTicket, submissionTime.Add(3*time.Minute), submissionTime.Add(3*time.Minute)); err != nil {
		t.Fatalf("dating newer ticket trace: %v", err)
	}

	newerBlankTicket := filepath.Join(dir, "newer-blank-ticket.md")
	if err := os.WriteFile(newerBlankTicket, []byte("MR URL: "+mrURL+"\nTicket URL: "), 0o600); err != nil {
		t.Fatalf("writing newer blank-ticket trace: %v", err)
	}
	if err := os.Chtimes(newerBlankTicket, submissionTime.Add(2*time.Minute), submissionTime.Add(2*time.Minute)); err != nil {
		t.Fatalf("dating newer blank-ticket trace: %v", err)
	}

	olderAbsentTicket := filepath.Join(dir, "older-absent-ticket.md")
	if err := os.WriteFile(olderAbsentTicket, []byte("MR URL: "+mrURL), 0o600); err != nil {
		t.Fatalf("writing older absent-ticket trace: %v", err)
	}
	if err := os.Chtimes(olderAbsentTicket, submissionTime.Add(time.Minute), submissionTime.Add(time.Minute)); err != nil {
		t.Fatalf("dating older absent-ticket trace: %v", err)
	}

	nonMatch := filepath.Join(dir, "non-match.md")
	if err := os.WriteFile(nonMatch, []byte("MR URL: https://gitlab.example.com/group/project/-/merge_requests/8"), 0o600); err != nil {
		t.Fatalf("writing non-matching trace: %v", err)
	}
	if err := os.Chtimes(nonMatch, submissionTime.Add(3*time.Minute), submissionTime.Add(3*time.Minute)); err != nil {
		t.Fatalf("dating non-matching trace: %v", err)
	}

	selected, err := selectNewestMatchingTrace(dir, map[string]time.Time{}, submissionTime, mrURL, "")
	if err != nil {
		t.Fatalf("selecting MR-only trace: %v", err)
	}

	if selected != newerBlankTicket {
		t.Fatalf("selected = %q, want %q", selected, newerBlankTicket)
	}
}

func submitReviewToTestServer(t *testing.T, input reviewInput) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	requestBodies := make(chan string, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		_ = r.Body.Close()
		requestBodies <- string(body)
		_, _ = w.Write([]byte("Reviewing MR"))
	})}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
	})

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("splitting listener address: %v", err)
	}

	if err := submitReview(context.Background(), port, "secret", input, "http://127.0.0.1/callback"); err != nil {
		t.Fatalf("submitting review: %v", err)
	}

	select {
	case body := <-requestBodies:
		return body
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request body")
	}
	return ""
}
