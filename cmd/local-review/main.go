package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultLocalPort = "8888"
	defaultTraceDir  = ".review-traces"
	serverLogPath    = ".local-review-server.log"
	healthTimeout    = 30 * time.Second
	waitTimeout      = 11 * time.Minute
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() error {
	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	env, err := loadDotEnv(repoRoot)
	if err != nil {
		return err
	}
	if err := requireLocalConfig(env); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	port := env["PORT"]
	healthURL := "http://127.0.0.1:" + port + "/health"
	startedServer := false
	var child *localServer
	if healthy(ctx, healthURL) {
		_, _ = fmt.Fprintf(os.Stderr, "Reusing healthy local server on port %s.\n", port)
	} else {
		child, err = startServer(ctx, repoRoot, env)
		if err != nil {
			return err
		}
		startedServer = true
		defer terminateChild(child)

		if err := waitForHealth(ctx, healthURL, healthTimeout); err != nil {
			return fmt.Errorf("local server did not become healthy within 30 seconds; see %s: %w", serverLogPath, err)
		}
	}

	stdin := bufio.NewReader(os.Stdin)
	input, err := promptReviewInputs(os.Stderr, stdin, env)
	if err != nil {
		return err
	}

	callback, err := startCallbackServer(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = callback.close(context.Background()) }()

	traceDir := env["REVIEW_TRACE_DIR"]
	before, err := snapshotTraceFiles(traceDir)
	if err != nil {
		return fmt.Errorf("recording existing trace files in %s: %w", traceDir, err)
	}
	submittedAt := time.Now()

	if err := submitReview(ctx, port, env["SLACK_SIGNING_SECRET"], input, callback.responseURL); err != nil {
		return err
	}

	if err := waitForCallback(ctx, callback.done, waitTimeout); err != nil {
		return err
	}

	tracePath, err := selectNewestMatchingTrace(traceDir, before, submittedAt, input.MRURL, input.TicketURL)
	if err != nil {
		return fmt.Errorf("extracting trace failed in %s after callback was received: %w", traceDir, err)
	}

	traceContent, err := os.ReadFile(tracePath)
	if err != nil {
		return fmt.Errorf("reading trace %s: %w", tracePath, err)
	}
	result, err := extractParsedReviewResult(traceContent)
	if err != nil {
		return fmt.Errorf("extracting parsed review result from %s failed; trace dir %s, callback was received: %w", tracePath, traceDir, err)
	}

	if startedServer {
		_, _ = fmt.Fprintln(os.Stderr, "Stopping local server started by this helper.")
	}
	_, _ = fmt.Fprintln(os.Stdout, renderParsedReviewResult(result))
	return nil
}

func renderParsedReviewResult(result string) string {
	lines := strings.Split(result, "\n")
	fenceLength := 0
	for i, line := range lines {
		currentFenceLength := markdownFenceLength(line)
		if fenceLength == 0 && currentFenceLength >= 3 {
			fenceLength = currentFenceLength
			continue
		}
		if fenceLength > 0 {
			if currentFenceLength >= fenceLength {
				fenceLength = 0
			}
			continue
		}
		level, heading, ok := parseMarkdownHeading(line)
		if ok {
			lines[i] = headingStyle(level) + heading + "\x1b[0m"
		}
	}
	return strings.Join(lines, "\n")
}

func markdownFenceLength(line string) int {
	trimmed := strings.TrimSpace(line)
	length := 0
	for length < len(trimmed) && trimmed[length] == '`' {
		length++
	}
	return length
}

func parseMarkdownHeading(line string) (int, string, bool) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level == len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimLeft(line[level:], " "), true
}

func headingStyle(level int) string {
	switch level {
	case 1:
		return "\x1b[1;35m"
	case 2:
		return "\x1b[1;34m"
	case 3:
		return "\x1b[1;36m"
	default:
		return "\x1b[1;33m"
	}
}

func loadDotEnv(repoRoot string) (map[string]string, error) {
	env := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			env[key] = value
		}
	}

	path := filepath.Join(repoRoot, ".env")
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			applyLocalDefaults(env)
			return env, nil
		}
		return nil, fmt.Errorf("opening .env: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := env[key]; !exists {
			env[key] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading .env: %w", err)
	}

	applyLocalDefaults(env)
	return env, nil
}

func applyLocalDefaults(env map[string]string) {
	if _, exists := env["PORT"]; !exists {
		env["PORT"] = defaultLocalPort
	}
	if _, exists := env["REVIEW_TRACE_DIR"]; !exists {
		env["REVIEW_TRACE_DIR"] = defaultTraceDir
	}
}

func requireLocalConfig(env map[string]string) error {
	if strings.TrimSpace(env["SLACK_SIGNING_SECRET"]) == "" {
		return fmt.Errorf("missing required local-review config: SLACK_SIGNING_SECRET")
	}
	return nil
}

func healthy(ctx context.Context, healthURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return err == nil && strings.Contains(string(body), "ok")
}

type localServer struct {
	cmd  *exec.Cmd
	done <-chan struct{}
}

func startServer(ctx context.Context, repoRoot string, env map[string]string) (*localServer, error) {
	logFile, err := os.OpenFile(filepath.Join(repoRoot, serverLogPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", serverLogPath, err)
	}

	childEnv := copyEnv(env)
	childEnv["REVIEW_TRACE_ENABLED"] = "true"
	if strings.TrimSpace(childEnv["REVIEW_TRACE_DIR"]) == "" {
		childEnv["REVIEW_TRACE_DIR"] = defaultTraceDir
	}

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/server")
	cmd.Dir = repoRoot
	cmd.Env = envMapToList(childEnv)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("starting local server; see %s: %w", serverLogPath, err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		close(done)
	}()
	return &localServer{cmd: cmd, done: done}, nil
}

func copyEnv(env map[string]string) map[string]string {
	copy := make(map[string]string, len(env))
	for key, value := range env {
		copy[key] = value
	}
	return copy
}

func envMapToList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]string, 0, len(keys))
	for _, key := range keys {
		list = append(list, key+"="+env[key])
	}
	return list
}

func waitForHealth(ctx context.Context, healthURL string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if healthy(ctx, healthURL) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("health check failed at %s", healthURL)
		case <-ticker.C:
		}
	}
}

func terminateChild(server *localServer) {
	if server == nil || server.cmd == nil || server.cmd.Process == nil {
		return
	}
	_ = server.cmd.Process.Signal(os.Interrupt)
	select {
	case <-server.done:
	case <-time.After(5 * time.Second):
		_ = server.cmd.Process.Kill()
		<-server.done
	}
}

type reviewInput struct {
	MRURL                 string
	TicketURL             string
	Model                 string
	ReasoningEffort       string
	AdditionalInstruction string
}

func promptReviewInputs(stderr io.Writer, stdin *bufio.Reader, env map[string]string) (reviewInput, error) {
	mrURL, err := promptRequired(stderr, stdin, "GitLab MR URL: ")
	if err != nil {
		return reviewInput{}, err
	}
	ticketURL, err := promptOptional(stderr, stdin, "Jira ticket URL (optional): ")
	if err != nil {
		return reviewInput{}, err
	}
	model, err := promptOptionalWithDefault(stderr, stdin, "Model override", env["OPENAI_MODEL"])
	if err != nil {
		return reviewInput{}, err
	}
	reasoningEffort, err := promptOptionalWithDefault(stderr, stdin, "Reasoning effort override", env["OPENAI_REASONING_EFFORT"])
	if err != nil {
		return reviewInput{}, err
	}
	reasoningEffort = normaliseReasoningEffort(reasoningEffort)
	if err := validateReasoningEffort(reasoningEffort); err != nil {
		return reviewInput{}, err
	}
	additionalInstruction, err := promptOptional(stderr, stdin, "Additional review instruction/comment (optional): ")
	if err != nil {
		return reviewInput{}, err
	}
	return reviewInput{
		MRURL:                 mrURL,
		TicketURL:             ticketURL,
		Model:                 model,
		ReasoningEffort:       reasoningEffort,
		AdditionalInstruction: additionalInstruction,
	}, nil
}

func promptRequired(stderr io.Writer, stdin *bufio.Reader, label string) (string, error) {
	_, _ = fmt.Fprint(stderr, label)
	value, err := stdin.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading prompt: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", strings.TrimSuffix(label, ": "))
	}
	return value, nil
}

func promptOptional(stderr io.Writer, stdin *bufio.Reader, label string) (string, error) {
	_, _ = fmt.Fprint(stderr, label)
	value, err := stdin.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading prompt: %w", err)
	}
	return strings.TrimSpace(value), nil
}

func promptOptionalWithDefault(stderr io.Writer, stdin *bufio.Reader, label, defaultValue string) (string, error) {
	promptLabel := label + ": "
	if strings.TrimSpace(defaultValue) != "" {
		promptLabel = label + " [" + strings.TrimSpace(defaultValue) + "]: "
	}
	value, err := promptOptional(stderr, stdin, promptLabel)
	if err != nil {
		return "", err
	}
	if value == "" {
		return strings.TrimSpace(defaultValue), nil
	}
	return value, nil
}

func normaliseReasoningEffort(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateReasoningEffort(value string) error {
	if value == "" {
		return nil
	}
	switch value {
	case "low", "medium", "high", "xhigh":
		return nil
	default:
		return fmt.Errorf("invalid reasoning effort %q; must be one of low, medium, high, xhigh", value)
	}
}

type callbackServer struct {
	server      *http.Server
	responseURL string
	done        <-chan struct{}
}

func startCallbackServer(ctx context.Context) (callbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return callbackServer{}, fmt.Errorf("starting callback listener: %w", err)
	}
	done := make(chan struct{})
	once := sync.Once{}
	mux := http.NewServeMux()
	mux.HandleFunc("/slack-response", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
		once.Do(func() { close(done) })
	})
	server := &http.Server{Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	go func() {
		_ = server.Serve(listener)
	}()
	return callbackServer{server: server, responseURL: "http://" + listener.Addr().String() + "/slack-response", done: done}, nil
}

func (s callbackServer) close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func submitReview(ctx context.Context, port, signingSecret string, input reviewInput, responseURL string) error {
	values := url.Values{}
	values.Set("text", reviewText(input))
	values.Set("response_url", responseURL)
	if input.Model != "" {
		values.Set("model", input.Model)
	}
	if input.ReasoningEffort != "" {
		values.Set("reasoning_effort", input.ReasoningEffort)
	}
	if input.AdditionalInstruction != "" {
		values.Set("additional_instruction", input.AdditionalInstruction)
	}
	rawBody := []byte(values.Encode())
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/slack/review", bytes.NewReader(rawBody))
	if err != nil {
		return fmt.Errorf("creating local review request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", signSlackRequest(signingSecret, timestamp, rawBody))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending local review request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !strings.Contains(string(body), "Reviewing MR") {
		_, _ = fmt.Fprintf(os.Stderr, "Initial request failed with HTTP %d:\n%s\n", resp.StatusCode, string(body))
		return fmt.Errorf("initial request failed")
	}
	return nil
}

func reviewText(input reviewInput) string {
	if input.TicketURL == "" {
		return input.MRURL
	}
	return input.MRURL + " " + input.TicketURL
}

func signSlackRequest(secret, timestamp string, rawBody []byte) string {
	base := "v0:" + timestamp + ":" + string(rawBody)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func waitForCallback(ctx context.Context, done <-chan struct{}, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	spinner := []string{"|", "/", "-", "\\"}
	i := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			_, _ = fmt.Fprint(os.Stderr, "\rReview callback received.        \n")
			return nil
		case <-timer.C:
			return fmt.Errorf("timed out after 11 minutes waiting for local callback")
		case <-ticker.C:
			_, _ = fmt.Fprintf(os.Stderr, "\rWaiting for review callback %s", spinner[i%len(spinner)])
			i++
		}
	}
}

func snapshotTraceFiles(traceDir string) (map[string]time.Time, error) {
	files := map[string]time.Time{}
	err := filepath.WalkDir(traceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[path] = info.ModTime()
		return nil
	})
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return files, nil
	}
	return files, err
}

func selectNewestMatchingTrace(traceDir string, before map[string]time.Time, submissionTime time.Time, mrURL, ticketURL string) (string, error) {
	type candidate struct {
		path    string
		modTime time.Time
	}
	var candidates []candidate
	err := filepath.WalkDir(traceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		previous, existed := before[path]
		if !info.ModTime().After(submissionTime) {
			return nil
		}
		if existed && !info.ModTime().After(previous) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		matchesMR := containsFieldLine(text, "MR URL", mrURL)
		matchesTicket := containsFieldLine(text, "Ticket URL", ticketURL)
		if ticketURL == "" {
			matchesTicket = hasBlankOrAbsentFieldLine(text, "Ticket URL")
		}
		if matchesMR && matchesTicket {
			candidates = append(candidates, candidate{path: path, modTime: info.ModTime()})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no new or modified trace matched the review input")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].path, nil
}

func containsFieldLine(content, field, value string) bool {
	want := field + ": " + value
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func hasBlankOrAbsentFieldLine(content, field string) bool {
	prefix := field + ":"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == prefix {
			return true
		}
		value, found := strings.CutPrefix(trimmed, prefix+" ")
		if found {
			return strings.TrimSpace(value) == ""
		}
	}
	return true
}

func extractParsedReviewResult(content []byte) (string, error) {
	text := string(content)
	startMarker := "## Parsed Review Result"
	endMarker := "## Final Slack Message"
	start := strings.Index(text, startMarker)
	if start < 0 {
		return "", fmt.Errorf("missing %q section", startMarker)
	}
	start += len(startMarker)
	end := strings.Index(text[start:], endMarker)
	if end < 0 {
		return "", fmt.Errorf("missing %q section", endMarker)
	}
	result := strings.TrimSpace(text[start : start+end])
	if result == "" {
		return "", fmt.Errorf("parsed review result section is empty")
	}
	return result, nil
}
