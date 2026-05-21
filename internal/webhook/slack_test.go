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
	if strings.Contains(rawURL, "gitlab.example.com") {
		return "platform/application", 108, true, nil
	}
	return "", 0, false, nil
}

type fakeJira struct{}

func (fakeJira) ClassifyTicketURL(rawURL string) (string, bool, error) {
	if strings.Contains(rawURL, "jira.example.com") {
		return "PROJ-141", true, nil
	}
	return "", false, nil
}

type fakeProcessor struct{ done chan review.Request }

func (f *fakeProcessor) Process(_ context.Context, req review.Request) {
	if f.done != nil {
		f.done <- req
	}
}
