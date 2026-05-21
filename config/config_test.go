package config

import "testing"

func TestLoadUsesDefaultsAndAllowsMissingSlackBotToken(t *testing.T) {
	t.Setenv("SLACK_SIGNING_SECRET", "secret")
	t.Setenv("GITLAB_TOKEN", "gitlab")
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	t.Setenv("JIRA_EMAIL", "user@example.com")
	t.Setenv("JIRA_TOKEN", "jira")
	t.Setenv("OPENAI_API_KEY", "openai")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != DefaultPort {
		t.Fatalf("Port = %q, want %q", cfg.Port, DefaultPort)
	}
	if cfg.GitLabBaseURL != DefaultGitLabBaseURL {
		t.Fatalf("GitLabBaseURL = %q, want %q", cfg.GitLabBaseURL, DefaultGitLabBaseURL)
	}
	if cfg.OpenAIModel != DefaultOpenAIModel {
		t.Fatalf("OpenAIModel = %q, want %q", cfg.OpenAIModel, DefaultOpenAIModel)
	}
	if cfg.ReviewTraceEnabled {
		t.Fatal("ReviewTraceEnabled = true, want false")
	}
	if cfg.ReviewTraceDir != DefaultReviewTraceDir {
		t.Fatalf("ReviewTraceDir = %q, want %q", cfg.ReviewTraceDir, DefaultReviewTraceDir)
	}
	if cfg.ReviewTraceIncludePrompts {
		t.Fatal("ReviewTraceIncludePrompts = true, want false")
	}
	if cfg.SlackBotToken != "" {
		t.Fatalf("SlackBotToken = %q, want empty", cfg.SlackBotToken)
	}
}

func TestLoadParsesReviewTraceEnvVars(t *testing.T) {
	t.Setenv("SLACK_SIGNING_SECRET", "secret")
	t.Setenv("GITLAB_TOKEN", "gitlab")
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	t.Setenv("JIRA_EMAIL", "user@example.com")
	t.Setenv("JIRA_TOKEN", "jira")
	t.Setenv("OPENAI_API_KEY", "openai")
	t.Setenv("REVIEW_TRACE_ENABLED", " YeS ")
	t.Setenv("REVIEW_TRACE_DIR", " traces ")
	t.Setenv("REVIEW_TRACE_INCLUDE_PROMPTS", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.ReviewTraceEnabled {
		t.Fatal("ReviewTraceEnabled = false, want true")
	}
	if cfg.ReviewTraceDir != "traces" {
		t.Fatalf("ReviewTraceDir = %q, want %q", cfg.ReviewTraceDir, "traces")
	}
	if !cfg.ReviewTraceIncludePrompts {
		t.Fatal("ReviewTraceIncludePrompts = false, want true")
	}
}

func TestLoadTreatsUnsupportedReviewTraceBooleansAsFalse(t *testing.T) {
	t.Setenv("SLACK_SIGNING_SECRET", "secret")
	t.Setenv("GITLAB_TOKEN", "gitlab")
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	t.Setenv("JIRA_EMAIL", "user@example.com")
	t.Setenv("JIRA_TOKEN", "jira")
	t.Setenv("OPENAI_API_KEY", "openai")
	t.Setenv("REVIEW_TRACE_ENABLED", "on")
	t.Setenv("REVIEW_TRACE_INCLUDE_PROMPTS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ReviewTraceEnabled {
		t.Fatal("ReviewTraceEnabled = true, want false")
	}
	if cfg.ReviewTraceIncludePrompts {
		t.Fatal("ReviewTraceIncludePrompts = true, want false")
	}
}

func TestLoadFailsWhenRequiredEnvVarsAreMissing(t *testing.T) {
	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing config error")
	}
	if !IsMissingConfigError(err) {
		t.Fatalf("IsMissingConfigError(%v) = false, want true", err)
	}
}

func TestTimeoutConstantsHaveApprovedValues(t *testing.T) {
	if ServerReadTimeout.String() != "5s" {
		t.Fatalf("ServerReadTimeout = %s", ServerReadTimeout)
	}
	if ServerWriteTimeout.String() != "10s" {
		t.Fatalf("ServerWriteTimeout = %s", ServerWriteTimeout)
	}
	if ExternalHTTPTimeout.String() != "20s" {
		t.Fatalf("ExternalHTTPTimeout = %s", ExternalHTTPTimeout)
	}
	if ReviewTimeout.String() != "10m0s" {
		t.Fatalf("ReviewTimeout = %s", ReviewTimeout)
	}
}
