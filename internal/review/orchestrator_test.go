package review

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/example/ai-pr-reviewer/internal/agents"
)

func TestOrchestratorPostsSuccess(t *testing.T) {
	poster := &fakePoster{}
	orchestrator := NewOrchestrator(fakeChanges{}, fakeTickets{}, fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "AI PR Review") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

func TestOrchestratorPostsSafeError(t *testing.T) {
	poster := &fakePoster{}
	orchestrator := NewOrchestrator(fakeChanges{err: errors.New("boom")}, fakeTickets{}, fakeReviewer{}, poster, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orchestrator.Process(context.Background(), Request{ResponseURL: "response", MRURL: "mr", ProjectPath: "org/repo", MRIID: 1, IssueKey: "PROJ-141"})

	if len(poster.messages) != 1 || !strings.Contains(poster.messages[0], "failed") {
		t.Fatalf("messages = %#v", poster.messages)
	}
}

type fakeChanges struct{ err error }

func (f fakeChanges) FetchChangeContext(context.Context, string, int) (string, bool, error) {
	return "diff", true, f.err
}

type fakeTickets struct{}

func (fakeTickets) FetchTicketContext(context.Context, string) (string, error) {
	return "ticket", nil
}

type fakeReviewer struct{}

func (fakeReviewer) Review(context.Context, string, string, bool) (agents.ReviewResult, error) {
	return agents.ReviewResult{TicketCoverage: ":white_check_mark: All criteria covered", Summary: "Looks good.", DiffTruncated: true}, nil
}

type fakePoster struct{ messages []string }

func (f *fakePoster) Post(_ context.Context, _ string, text string) error {
	f.messages = append(f.messages, text)
	return nil
}
