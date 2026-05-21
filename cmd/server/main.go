package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/example/ai-pr-reviewer/config"
	"github.com/example/ai-pr-reviewer/internal/agents"
	"github.com/example/ai-pr-reviewer/internal/gitlab"
	"github.com/example/ai-pr-reviewer/internal/jira"
	"github.com/example/ai-pr-reviewer/internal/review"
	"github.com/example/ai-pr-reviewer/internal/slack"
	"github.com/example/ai-pr-reviewer/internal/trace"
	"github.com/example/ai-pr-reviewer/internal/webhook"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("loading config failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	skills, err := agents.LoadSkills("skills")
	if err != nil {
		logger.Error("loading skills failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: config.ExternalHTTPTimeout}
	gitlabClient, err := gitlab.NewClient(cfg.GitLabBaseURL, cfg.GitLabToken, httpClient)
	if err != nil {
		logger.Error("creating GitLab client failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	jiraClient, err := jira.NewClient(cfg.JiraBaseURL, cfg.JiraEmail, cfg.JiraToken, httpClient)
	if err != nil {
		logger.Error("creating Jira client failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	reviewer := agents.NewOpenAIReviewer(cfg.OpenAIAPIKey, cfg.OpenAIModel, skills, cfg.ReviewTraceIncludePrompts)
	poster := slack.NewPoster(httpClient)
	orchestratorOptions := []review.OrchestratorOption{}
	if cfg.ReviewTraceEnabled {
		traceWriter := trace.NewWriter(cfg.ReviewTraceEnabled, cfg.ReviewTraceDir, cfg.ReviewTraceIncludePrompts, []string{
			cfg.SlackSigningSecret,
			cfg.SlackBotToken,
			cfg.GitLabToken,
			cfg.JiraToken,
			cfg.OpenAIAPIKey,
		})
		orchestratorOptions = append(orchestratorOptions, review.WithTraceWriter(traceWriter))
	}
	orchestrator := review.NewOrchestrator(gitlabClient, jiraClient, reviewer, poster, logger, orchestratorOptions...)
	handler := webhook.NewHandler(cfg.SlackSigningSecret, gitlabClient, jiraClient, orchestrator, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/slack/review", handler)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  config.ServerReadTimeout,
		WriteTimeout: config.ServerWriteTimeout,
	}

	logger.Info("starting server", slog.String("port", cfg.Port))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
