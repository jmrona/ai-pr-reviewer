package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/ai-pr-reviewer/internal/review"
)

func TestHandlerRejectsInvalidSignature(t *testing.T) {
	handler := newTestHandler(&fakeProcessor{})
	req := httptest.NewRequest(http.MethodPost, "/slack/review", strings.NewReader("text=x"))
	req.Header.Set("X-Slack-Signature", "v0=bad")
	req.Header.Set("X-Slack-Request-Timestamp", "1700000000")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandlerAcceptsURLsInEitherOrder(t *testing.T) {
	for _, text := range []string{
		"https://jira.example.com/browse/PROJ-141 https://gitlab.example.com/platform/application/-/merge_requests/108",
		"https://gitlab.example.com/platform/application/-/merge_requests/108 https://jira.example.com/browse/PROJ-141",
	} {
		processor := &fakeProcessor{done: make(chan review.Request, 1)}
		handler := newTestHandler(processor)
		body := url.Values{"text": {text}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
		req := signedRequest(body, handler.signingSecret, handler.now())

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Reviewing MR") {
			t.Fatalf("body = %q, want reviewing acknowledgement", rec.Body.String())
		}

		select {
		case got := <-processor.done:
			if got.IssueKey != "PROJ-141" || got.ProjectPath != "platform/application" || got.MRIID != 108 {
				t.Fatalf("request = %#v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("processor was not called")
		}
	}
}

func TestHandlerReturnsUsageForAmbiguousURLs(t *testing.T) {
	handler := newTestHandler(&fakeProcessor{done: make(chan review.Request, 1)})
	body := url.Values{"text": {"https://jira.example.com/browse/PROJ-141 https://jira.example.com/browse/PROJ-142"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	req := signedRequest(body, handler.signingSecret, handler.now())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "two Jira") {
		t.Fatalf("body = %q, want two Jira error", rec.Body.String())
	}
}

func TestHandlerRejectsDuplicateInFlightReviewUsingParsedKey(t *testing.T) {
	processor := &blockingProcessor{
		started:  make(chan review.Request, 2),
		release:  make(chan struct{}),
		finished: make(chan struct{}, 2),
	}
	t.Cleanup(func() { close(processor.release) })
	handler := newTestHandler(processor)

	firstBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108?ignored=one https://jira.example.com/browse/PROJ-141?ignored=one"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	firstReq := signedRequest(firstBody, handler.signingSecret, handler.now())
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)

	if firstRec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", firstRec.Code)
	}
	if !strings.Contains(firstRec.Body.String(), "Reviewing MR") {
		t.Fatalf("first body = %q, want reviewing acknowledgement", firstRec.Body.String())
	}
	select {
	case got := <-processor.started:
		if got.IssueKey != "PROJ-141" || got.ProjectPath != "platform/application" || got.MRIID != 108 {
			t.Fatalf("request = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}

	secondBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108?ignored=two https://jira.example.com/browse/PROJ-141?ignored=two"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	secondReq := signedRequest(secondBody, handler.signingSecret, handler.now())
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200", secondRec.Code)
	}
	if !strings.Contains(secondRec.Body.String(), "I'm still reviewing that merge request") {
		t.Fatalf("second body = %q, want duplicate in-flight message", secondRec.Body.String())
	}
	select {
	case got := <-processor.started:
		t.Fatalf("processor was called for duplicate request: %#v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHandlerAllowsReviewAfterInFlightReviewFinishes(t *testing.T) {
	release := make(chan struct{})
	releaseClosed := false
	processor := &blockingProcessor{
		started:  make(chan review.Request, 2),
		release:  release,
		finished: make(chan struct{}, 2),
	}
	t.Cleanup(func() {
		if !releaseClosed {
			close(release)
		}
	})
	handler := newTestHandler(processor)
	body := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108 https://jira.example.com/browse/PROJ-141"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()

	firstReq := signedRequest(body, handler.signingSecret, handler.now())
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)
	if !strings.Contains(firstRec.Body.String(), "Reviewing MR") {
		t.Fatalf("first body = %q, want reviewing acknowledgement", firstRec.Body.String())
	}
	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("first processor call did not start")
	}
	close(release)
	releaseClosed = true
	select {
	case <-processor.finished:
	case <-time.After(time.Second):
		t.Fatal("first processor call did not finish")
	}

	secondReq := signedRequest(body, handler.signingSecret, handler.now())
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)
	if !strings.Contains(secondRec.Body.String(), "Reviewing MR") {
		t.Fatalf("second body = %q, want reviewing acknowledgement", secondRec.Body.String())
	}
	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("second processor call did not start")
	}
}

func TestHandlerAllowsSameMergeRequestWithDifferentJiraTicket(t *testing.T) {
	processor := &blockingProcessor{
		started:  make(chan review.Request, 2),
		release:  make(chan struct{}),
		finished: make(chan struct{}, 2),
	}
	t.Cleanup(func() { close(processor.release) })
	handler := newTestHandler(processor)

	firstBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108 https://jira.example.com/browse/PROJ-141"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	firstReq := signedRequest(firstBody, handler.signingSecret, handler.now())
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)
	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("first processor call did not start")
	}

	secondBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108 https://jira.example.com/browse/PROJ-142"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	secondReq := signedRequest(secondBody, handler.signingSecret, handler.now())
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)
	if !strings.Contains(secondRec.Body.String(), "Reviewing MR") {
		t.Fatalf("second body = %q, want reviewing acknowledgement", secondRec.Body.String())
	}
	select {
	case got := <-processor.started:
		if got.IssueKey != "PROJ-142" || got.MRIID != 108 {
			t.Fatalf("request = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("second processor call did not start")
	}
}

func TestHandlerAllowsSameJiraTicketWithDifferentMergeRequest(t *testing.T) {
	processor := &blockingProcessor{
		started:  make(chan review.Request, 2),
		release:  make(chan struct{}),
		finished: make(chan struct{}, 2),
	}
	t.Cleanup(func() { close(processor.release) })
	handler := newTestHandler(processor)

	firstBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108 https://jira.example.com/browse/PROJ-141"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	firstReq := signedRequest(firstBody, handler.signingSecret, handler.now())
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)
	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("first processor call did not start")
	}

	secondBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/109 https://jira.example.com/browse/PROJ-141"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	secondReq := signedRequest(secondBody, handler.signingSecret, handler.now())
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)
	if !strings.Contains(secondRec.Body.String(), "Reviewing MR") {
		t.Fatalf("second body = %q, want reviewing acknowledgement", secondRec.Body.String())
	}
	select {
	case got := <-processor.started:
		if got.IssueKey != "PROJ-141" || got.MRIID != 109 {
			t.Fatalf("request = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("second processor call did not start")
	}
}

func TestHandlerDoesNotTrackInvalidRequests(t *testing.T) {
	processor := &fakeProcessor{done: make(chan review.Request, 1)}
	handler := newTestHandler(processor)
	invalidBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108 not-a-jira-ticket"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	invalidReq := signedRequest(invalidBody, handler.signingSecret, handler.now())
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)

	if !strings.Contains(invalidRec.Body.String(), "Expected one Jira ticket URL") {
		t.Fatalf("invalid body = %q, want usage error", invalidRec.Body.String())
	}
	select {
	case got := <-processor.done:
		t.Fatalf("processor was called for invalid request: %#v", got)
	case <-time.After(50 * time.Millisecond):
	}

	validBody := url.Values{"text": {"https://gitlab.example.com/platform/application/-/merge_requests/108 https://jira.example.com/browse/PROJ-141"}, "response_url": {"https://hooks.slack.com/response"}}.Encode()
	validReq := signedRequest(validBody, handler.signingSecret, handler.now())
	validRec := httptest.NewRecorder()
	handler.ServeHTTP(validRec, validReq)

	if !strings.Contains(validRec.Body.String(), "Reviewing MR") {
		t.Fatalf("valid body = %q, want reviewing acknowledgement", validRec.Body.String())
	}
	select {
	case got := <-processor.done:
		if got.IssueKey != "PROJ-141" || got.ProjectPath != "platform/application" || got.MRIID != 108 {
			t.Fatalf("request = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("processor was not called for valid request")
	}
}

func newTestHandler(processor ReviewProcessor) *Handler {
	handler := NewHandler("secret", fakeGitLab{}, fakeJira{}, processor, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler.now = func() time.Time { return time.Unix(1700000000, 0) }
	return handler
}

func signedRequest(body, secret string, now time.Time) *http.Request {
	timestamp := strconvFormat(now.Unix())
	base := "v0:" + timestamp + ":" + body
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	signature := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/slack/review", strings.NewReader(body))
	req.Header.Set("X-Slack-Signature", signature)
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func strconvFormat(value int64) string {
	return strconv.FormatInt(value, 10)
}

type fakeGitLab struct{}

func (fakeGitLab) ClassifyMRURL(rawURL string) (string, int, bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, false, err
	}
	if parsed.Host != "gitlab.example.com" {
		return "", 0, false, nil
	}
	marker := "/-/merge_requests/"
	index := strings.Index(parsed.Path, marker)
	if index == -1 {
		return "", 0, false, nil
	}
	mrIID, err := strconv.Atoi(strings.Trim(parsed.Path[index+len(marker):], "/"))
	if err != nil {
		return "", 0, false, err
	}
	return strings.TrimPrefix(parsed.Path[:index], "/"), mrIID, true, nil
}

type fakeJira struct{}

func (fakeJira) ClassifyTicketURL(rawURL string) (string, bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false, err
	}
	if parsed.Host != "jira.example.com" {
		return "", false, nil
	}
	issueKey := strings.TrimPrefix(parsed.Path, "/browse/")
	if issueKey == parsed.Path || issueKey == "" {
		return "", false, nil
	}
	return issueKey, true, nil
}

type fakeProcessor struct{ done chan review.Request }

func (f *fakeProcessor) Process(_ context.Context, req review.Request) {
	if f.done != nil {
		f.done <- req
	}
}

type blockingProcessor struct {
	started  chan review.Request
	release  chan struct{}
	finished chan struct{}
}

func (p *blockingProcessor) Process(_ context.Context, req review.Request) {
	p.started <- req
	<-p.release
	p.finished <- struct{}{}
}
