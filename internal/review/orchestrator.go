package review

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/example/ai-pr-reviewer/internal/agents"
	"github.com/example/ai-pr-reviewer/internal/slack"
)

type Request struct {
	ResponseURL string
	MRURL       string
	ProjectPath string
	MRIID       int
	IssueKey    string
}

type ChangeFetcher interface {
	FetchChangeContext(ctx context.Context, projectPath string, mrIID int) (string, bool, error)
}

type TicketFetcher interface {
	FetchTicketContext(ctx context.Context, issueKey string) (string, error)
}

type Reviewer interface {
	Review(ctx context.Context, ticketContext, diff string, diffTruncated bool) (agents.ReviewResult, error)
}

type SlackPoster interface {
	Post(ctx context.Context, responseURL, text string) error
}

type Orchestrator struct {
	changes  ChangeFetcher
	tickets  TicketFetcher
	reviewer Reviewer
	poster   SlackPoster
	logger   *slog.Logger
}

func NewOrchestrator(changes ChangeFetcher, tickets TicketFetcher, reviewer Reviewer, poster SlackPoster, logger *slog.Logger) *Orchestrator {
	return &Orchestrator{changes: changes, tickets: tickets, reviewer: reviewer, poster: poster, logger: logger}
}

func (o *Orchestrator) Process(ctx context.Context, req Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			o.logger.ErrorContext(ctx, "review panicked", slog.Any("panic", recovered), slog.String("stack", string(debug.Stack())))
			o.postError(ctx, req.ResponseURL, "the review crashed unexpectedly")
		}
	}()

	if err := o.process(ctx, req); err != nil {
		o.logger.ErrorContext(ctx, "review failed", slog.String("error", err.Error()))
		o.postError(ctx, req.ResponseURL, safeUserError(err))
	}
}

func (o *Orchestrator) process(ctx context.Context, req Request) error {
	o.logger.InfoContext(ctx, "fetching GitLab changes", slog.String("project_path", req.ProjectPath), slog.Int("mr_iid", req.MRIID))
	diff, truncated, err := o.changes.FetchChangeContext(ctx, req.ProjectPath, req.MRIID)
	if err != nil {
		return fmt.Errorf("GitLab fetch failed: %w", err)
	}

	o.logger.InfoContext(ctx, "fetching Jira issue", slog.String("issue_key", req.IssueKey))
	ticketContext, err := o.tickets.FetchTicketContext(ctx, req.IssueKey)
	if err != nil {
		return fmt.Errorf("Jira fetch failed: %w", err)
	}

	o.logger.InfoContext(ctx, "running OpenAI review")
	result, err := o.reviewer.Review(ctx, ticketContext, diff, truncated)
	if err != nil {
		return fmt.Errorf("OpenAI review failed: %w", err)
	}
	result.DiffTruncated = truncated

	message := slack.FormatReviewResult(result, req.IssueKey, req.MRURL)
	o.logger.InfoContext(ctx, "posting Slack review result")
	if err := o.poster.Post(ctx, req.ResponseURL, message); err != nil {
		return fmt.Errorf("Slack callback failed: %w", err)
	}

	return nil
}

func (o *Orchestrator) postError(ctx context.Context, responseURL, message string) {
	if err := o.poster.Post(ctx, responseURL, slack.FormatError(message)); err != nil {
		o.logger.ErrorContext(ctx, "posting Slack error failed", slog.String("error", err.Error()))
	}
}

func safeUserError(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}
