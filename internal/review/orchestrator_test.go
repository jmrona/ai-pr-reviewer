package review

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/example/ai-pr-reviewer/internal/agents"
	"github.com/example/ai-pr-reviewer/internal/trace"
)

func TestOrchestratorPostsSuccess(t *testing.T) {
	poster := &fakePoster{}
	orchestrator := NewOrchestrator(fakeChanges{}, &fakeTickets{}, &fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "AI PR Review") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

func TestOrchestratorWritesTraceOnSuccess(t *testing.T) {
	poster := &fakePoster{}
	traceWriter := &fakeTraceWriter{path: "trace.md"}
	orchestrator := NewOrchestrator(fakeChanges{}, &fakeTickets{}, &fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)), WithTraceWriter(traceWriter))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	if len(traceWriter.inputs) != 1 {
		t.Fatalf("trace writes = %d, want 1", len(traceWriter.inputs))
	}
	input := traceWriter.inputs[0]
	if input.IssueKey != "PROJ-141" || input.MRURL != "mr" || input.TicketContext != "ticket" || input.Diff != "diff" {
		t.Fatalf("trace input = %#v", input)
	}
	if !input.DiffTruncated || !input.ReviewOutcome.Result.DiffTruncated {
		t.Fatalf("trace truncation = input %t result %t, want both true", input.DiffTruncated, input.ReviewOutcome.Result.DiffTruncated)
	}
	if !strings.Contains(input.SlackMessage, "AI PR Review") {
		t.Fatalf("trace Slack message = %q", input.SlackMessage)
	}
	if len(poster.messages) != 1 || poster.messages[0] != input.SlackMessage {
		t.Fatalf("messages = %#v, trace message = %q", poster.messages, input.SlackMessage)
	}
}

func TestOrchestratorWritesNoTraceWhenTraceWriterIsNil(t *testing.T) {
	poster := &fakePoster{}
	orchestrator := NewOrchestrator(fakeChanges{}, &fakeTickets{}, &fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "AI PR Review") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

func TestOrchestratorSkipsJiraFetchWhenIssueKeyIsEmpty(t *testing.T) {
	poster := &fakePoster{}
	reviewer := &fakeReviewer{}
	tickets := &fakeTickets{err: errors.New("unexpected Jira fetch")}
	orchestrator := NewOrchestrator(fakeChanges{}, tickets, reviewer, poster, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1})

	if tickets.calls != 0 {
		t.Fatalf("Jira fetch calls = %d, want 0", tickets.calls)
	}
	if reviewer.ticketContext != "No Jira ticket context was provided for this review." {
		t.Fatalf("ticket context = %q", reviewer.ticketContext)
	}
	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "AI PR Review") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

func TestOrchestratorPassesReviewOptionsToReviewer(t *testing.T) {
	poster := &fakePoster{}
	reviewer := &fakeReviewer{}
	orchestrator := NewOrchestrator(fakeChanges{}, &fakeTickets{}, reviewer, poster, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orchestrator.Process(context.Background(), Request{
		ResponseURL:           "response",
		MRURL:                 "mr",
		ProjectPath:           "org/repo",
		MRIID:                 1,
		IssueKey:              "PROJ-141",
		Model:                 "gpt-5.5",
		ReasoningEffort:       "high",
		AdditionalInstruction: "Only review auth changes.",
	})

	want := agents.ReviewOptions{Model: "gpt-5.5", ReasoningEffort: "high", AdditionalInstruction: "Only review auth changes."}
	if reviewer.options != want {
		t.Fatalf("review options = %#v, want %#v", reviewer.options, want)
	}
}

func TestOrchestratorWritesNoTraceWhenTraceWriterIsDisabled(t *testing.T) {
	poster := &fakePoster{}
	dir := t.TempDir()
	traceWriter := trace.NewWriter(false, dir, false, nil)
	orchestrator := NewOrchestrator(fakeChanges{}, &fakeTickets{}, &fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)), WithTraceWriter(traceWriter))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading trace dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("trace files = %d, want 0", len(entries))
	}
	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "AI PR Review") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

func TestOrchestratorStillPostsSlackResultWhenTraceWriterReturnsError(t *testing.T) {
	poster := &fakePoster{}
	traceWriter := &fakeTraceWriter{err: errors.New("trace failed")}
	orchestrator := NewOrchestrator(fakeChanges{}, &fakeTickets{}, &fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)), WithTraceWriter(traceWriter))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	if len(traceWriter.inputs) != 1 {
		t.Fatalf("trace writes = %d, want 1", len(traceWriter.inputs))
	}
	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "AI PR Review") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

func TestOrchestratorPostsSafeError(t *testing.T) {
	poster := &fakePoster{}
	orchestrator := NewOrchestrator(fakeChanges{err: errors.New("boom")}, &fakeTickets{}, &fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "failed") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

type fakeChanges struct{ err error }

func (f fakeChanges) FetchChangeContext(context.Context, string, int) (string, bool, error) {
	return "diff", true, f.err
}

type fakeTickets struct {
	calls int
	err   error
}

func (f *fakeTickets) FetchTicketContext(context.Context, string) (string, error) {
	f.calls++
	return "ticket", f.err
}

type fakeReviewer struct {
	ticketContext string
	options       agents.ReviewOptions
}

func (f *fakeReviewer) Review(_ context.Context, ticketContext, _ string, _ bool, options agents.ReviewOptions) (agents.ReviewOutcome, error) {
	f.ticketContext = ticketContext
	f.options = options
	return agents.ReviewOutcome{Result: agents.ReviewResult{TicketCoverage: ":white_check_mark: All criteria covered", Summary: "Looks good.", DiffTruncated: true}}, nil
}

type fakePoster struct{ messages []string }

func (f *fakePoster) Post(_ context.Context, _ string, text string) error {
	f.messages = append(f.messages, text)
	return nil
}

type fakeTraceWriter struct {
	path   string
	err    error
	inputs []trace.TraceInput
}

func (f *fakeTraceWriter) Write(_ context.Context, input trace.TraceInput) (string, error) {
	f.inputs = append(f.inputs, input)
	return f.path, f.err
}
