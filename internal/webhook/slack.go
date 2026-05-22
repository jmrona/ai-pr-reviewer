package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/ai-pr-reviewer/config"
	"github.com/example/ai-pr-reviewer/internal/review"
)

type GitLabClassifier interface {
	ClassifyMRURL(rawURL string) (projectPath string, mrIID int, ok bool, err error)
}

type JiraClassifier interface {
	ClassifyTicketURL(rawURL string) (issueKey string, ok bool, err error)
}

type ReviewProcessor interface {
	Process(ctx context.Context, req review.Request)
}

type Handler struct {
	signingSecret string
	gitlab        GitLabClassifier
	jira          JiraClassifier
	processor     ReviewProcessor
	logger        *slog.Logger
	now           func() time.Time
	inFlight      *inFlightReviewTracker
}

func NewHandler(signingSecret string, gitlab GitLabClassifier, jira JiraClassifier, processor ReviewProcessor, logger *slog.Logger) *Handler {
	return &Handler{signingSecret: signingSecret, gitlab: gitlab, jira: jira, processor: processor, logger: logger, now: time.Now, inFlight: newInFlightReviewTracker()}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	if err := h.verifySignature(r.Header, body); err != nil {
		http.Error(w, "invalid Slack signature", http.StatusUnauthorized)
		return
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		writeSlackMessage(w, "Usage: /review <gitlab-mr-url> <jira-ticket-url>")
		return
	}

	responseURL := values.Get("response_url")
	if strings.TrimSpace(responseURL) == "" {
		writeSlackMessage(w, "Slack did not provide a response_url, so I cannot post the review result.")
		return
	}

	parsed, err := h.parseCommand(values.Get("text"), responseURL)
	if err != nil {
		writeSlackMessage(w, err.Error())
		return
	}
	if err := populateOptions(&parsed, values); err != nil {
		writeSlackMessage(w, err.Error())
		return
	}
	key := inFlightReviewKey{projectPath: parsed.ProjectPath, mrIID: parsed.MRIID, issueKey: parsed.IssueKey}
	if !h.inFlight.tryAcquire(key) {
		writeSlackMessage(w, ":hourglass_flowing_sand: I'm still reviewing that merge request. Wait for my review before asking me to review it again.")
		return
	}

	h.logger.InfoContext(r.Context(), "review command accepted", slog.String("issue_key", parsed.IssueKey), slog.String("project_path", parsed.ProjectPath), slog.Int("mr_iid", parsed.MRIID))
	writeSlackMessage(w, ":mag: Reviewing MR... I'll post results here shortly.")

	go func() {
		defer h.inFlight.release(key)
		ctx, cancel := context.WithTimeout(context.Background(), config.ReviewTimeout)
		defer cancel()
		h.processor.Process(ctx, parsed)
	}()
}

type inFlightReviewKey struct {
	projectPath string
	mrIID       int
	issueKey    string
}

type inFlightReviewTracker struct {
	mu      sync.Mutex
	reviews map[inFlightReviewKey]struct{}
}

func newInFlightReviewTracker() *inFlightReviewTracker {
	return &inFlightReviewTracker{reviews: make(map[inFlightReviewKey]struct{})}
}

func (t *inFlightReviewTracker) tryAcquire(key inFlightReviewKey) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.reviews[key]; exists {
		return false
	}
	t.reviews[key] = struct{}{}
	return true
}

func (t *inFlightReviewTracker) release(key inFlightReviewKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.reviews, key)
}

func (h *Handler) verifySignature(header http.Header, body []byte) error {
	signature := header.Get("X-Slack-Signature")
	timestamp := header.Get("X-Slack-Request-Timestamp")
	if signature == "" || timestamp == "" {
		return fmt.Errorf("missing Slack signature headers")
	}

	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Slack timestamp: %w", err)
	}
	requestTime := time.Unix(seconds, 0)
	if h.now().Sub(requestTime) > 5*time.Minute || requestTime.Sub(h.now()) > 5*time.Minute {
		return fmt.Errorf("stale Slack timestamp")
	}

	base := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(h.signingSecret))
	mac.Write([]byte(base))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("Slack signature mismatch")
	}

	return nil
}

func (h *Handler) parseCommand(text, responseURL string) (review.Request, error) {
	args := strings.Fields(text)
	if len(args) < 1 || len(args) > 2 {
		return review.Request{}, fmt.Errorf("Usage: /review <gitlab-mr-url> [jira-ticket-url]. The URLs can be in either order.")
	}

	var req review.Request
	var foundMR bool
	var foundTicket bool

	for _, arg := range args {
		projectPath, mrIID, isMR, err := h.gitlab.ClassifyMRURL(arg)
		if err != nil {
			return review.Request{}, fmt.Errorf("Invalid GitLab MR URL: %w", err)
		}
		if isMR {
			if foundMR {
				return review.Request{}, fmt.Errorf("Expected one Jira ticket URL and one GitLab MR URL, but received two GitLab MR URLs.")
			}
			req.MRURL = arg
			req.ProjectPath = projectPath
			req.MRIID = mrIID
			foundMR = true
			continue
		}

		issueKey, isTicket, err := h.jira.ClassifyTicketURL(arg)
		if err != nil {
			return review.Request{}, fmt.Errorf("Invalid Jira ticket URL: %w", err)
		}
		if isTicket {
			if foundTicket {
				return review.Request{}, fmt.Errorf("Expected one Jira ticket URL and one GitLab MR URL, but received two Jira ticket URLs.")
			}
			req.IssueKey = issueKey
			req.TicketURL = arg
			foundTicket = true
			continue
		}

		if len(args) == 2 {
			return review.Request{}, fmt.Errorf("Invalid Jira ticket URL: Expected one Jira ticket URL as the optional second argument.")
		}
	}

	if !foundMR {
		return review.Request{}, fmt.Errorf("Expected one GitLab MR URL and optional Jira ticket URL. The URLs can be in either order.")
	}
	req.ResponseURL = responseURL

	return req, nil
}

func populateOptions(req *review.Request, values url.Values) error {
	req.Model = strings.TrimSpace(values.Get("model"))
	req.ReasoningEffort = strings.ToLower(strings.TrimSpace(values.Get("reasoning_effort")))
	req.AdditionalInstruction = strings.TrimSpace(values.Get("additional_instruction"))
	reviewRounds, err := parseReviewRounds(values.Get("review_rounds"))
	if err != nil {
		return err
	}
	req.ReviewRounds = reviewRounds

	if req.ReasoningEffort == "" {
		return nil
	}
	for _, allowed := range []string{"low", "medium", "high", "xhigh"} {
		if req.ReasoningEffort == allowed {
			return nil
		}
	}
	return fmt.Errorf("reasoning_effort must be one of: low, medium, high, xhigh")
}

func parseReviewRounds(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	rounds, err := strconv.Atoi(trimmed)
	if err != nil || (rounds != 1 && rounds != 2) {
		return 0, fmt.Errorf("review_rounds must be 1 or 2")
	}
	return rounds, nil
}

func writeSlackMessage(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"response_type": "in_channel", "text": text})
}
