package review

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/example/ai-pr-reviewer/internal/agents"
	"github.com/example/ai-pr-reviewer/internal/slack"
	"github.com/example/ai-pr-reviewer/internal/trace"
)

type Request struct {
	ResponseURL           string
	MRURL                 string
	TicketURL             string
	ProjectPath           string
	MRIID                 int
	IssueKey              string
	Model                 string
	ReasoningEffort       string
	ReviewRounds          int
	AdditionalInstruction string
}

type ChangeFetcher interface {
	FetchChangeContext(ctx context.Context, projectPath string, mrIID int) (string, bool, error)
}

type TicketFetcher interface {
	FetchTicketContext(ctx context.Context, issueKey string) (string, error)
}

type Reviewer interface {
	Review(ctx context.Context, ticketContext, diff string, diffTruncated bool, options agents.ReviewOptions) (agents.ReviewOutcome, error)
}

type SlackPoster interface {
	Post(ctx context.Context, responseURL, text string) error
}

type TraceWriter interface {
	Write(ctx context.Context, input trace.TraceInput) (string, error)
}

type OrchestratorOption func(*Orchestrator)

type Orchestrator struct {
	changes     ChangeFetcher
	tickets     TicketFetcher
	reviewer    Reviewer
	poster      SlackPoster
	logger      *slog.Logger
	traceWriter TraceWriter
}

const noTicketContext = "No Jira ticket context was provided for this review."

func NewOrchestrator(changes ChangeFetcher, tickets TicketFetcher, reviewer Reviewer, poster SlackPoster, logger *slog.Logger, options ...OrchestratorOption) *Orchestrator {
	orchestrator := &Orchestrator{changes: changes, tickets: tickets, reviewer: reviewer, poster: poster, logger: logger}
	for _, option := range options {
		option(orchestrator)
	}
	return orchestrator
}

func WithTraceWriter(traceWriter TraceWriter) OrchestratorOption {
	return func(orchestrator *Orchestrator) {
		orchestrator.traceWriter = traceWriter
	}
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

	ticketContext := noTicketContext
	if req.IssueKey != "" {
		o.logger.InfoContext(ctx, "fetching Jira issue", slog.String("issue_key", req.IssueKey))
		var err error
		ticketContext, err = o.tickets.FetchTicketContext(ctx, req.IssueKey)
		if err != nil {
			return fmt.Errorf("Jira fetch failed: %w", err)
		}
	}

	o.logger.InfoContext(ctx, "running OpenAI review")
	outcome, err := o.reviewer.Review(ctx, ticketContext, diff, truncated, agents.ReviewOptions{
		Model:                 req.Model,
		ReasoningEffort:       req.ReasoningEffort,
		ReviewRounds:          req.ReviewRounds,
		AdditionalInstruction: req.AdditionalInstruction,
	})
	if err != nil {
		return fmt.Errorf("OpenAI review failed: %w", err)
	}
	result := outcome.Result
	result.DiffTruncated = truncated
	outcome.Result = result

	message := slack.FormatReviewResult(result, req.IssueKey, req.MRURL)
	o.writeTrace(ctx, req, ticketContext, diff, truncated, outcome, message)

	o.logger.InfoContext(ctx, "posting Slack review result")
	if err := o.poster.Post(ctx, req.ResponseURL, message); err != nil {
		return fmt.Errorf("Slack callback failed: %w", err)
	}

	return nil
}

func (o *Orchestrator) writeTrace(ctx context.Context, req Request, ticketContext, diff string, truncated bool, outcome agents.ReviewOutcome, message string) {
	if o.traceWriter == nil {
		return
	}

	path, err := o.traceWriter.Write(ctx, trace.TraceInput{
		IssueKey:              req.IssueKey,
		MRURL:                 req.MRURL,
		TicketURL:             req.TicketURL,
		AdditionalInstruction: req.AdditionalInstruction,
		TicketContext:         ticketContext,
		Diff:                  diff,
		DiffTruncated:         truncated,
		ReviewOutcome:         outcome,
		SlackMessage:          message,
		CreatedAt:             time.Now(),
	})
	if err != nil {
		o.logger.ErrorContext(ctx, "writing review trace failed", slog.String("error", err.Error()))
		return
	}
	if path != "" {
		o.logger.InfoContext(ctx, "wrote review trace", slog.String("path", path))
	}
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
