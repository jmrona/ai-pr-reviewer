package config

import (
	"strconv"
	"strings"
	"testing"
)

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
	if cfg.OpenAIReasoningEffort != "" {
		t.Fatalf("OpenAIReasoningEffort = %q, want empty", cfg.OpenAIReasoningEffort)
	}
	if cfg.OpenAIReviewRounds != 2 {
		t.Fatalf("OpenAIReviewRounds = %d, want 2", cfg.OpenAIReviewRounds)
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

func TestLoadAcceptsConfiguredOpenAIReviewRounds(t *testing.T) {
	for _, rounds := range []int{1, 2} {
		t.Run(strconv.Itoa(rounds), func(t *testing.T) {
			t.Setenv("SLACK_SIGNING_SECRET", "secret")
			t.Setenv("GITLAB_TOKEN", "gitlab")
			t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
			t.Setenv("JIRA_EMAIL", "user@example.com")
			t.Setenv("JIRA_TOKEN", "jira")
			t.Setenv("OPENAI_API_KEY", "openai")
			t.Setenv("OPENAI_REVIEW_ROUNDS", " "+strconv.Itoa(rounds)+" ")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.OpenAIReviewRounds != rounds {
				t.Fatalf("OpenAIReviewRounds = %d, want %d", cfg.OpenAIReviewRounds, rounds)
			}
		})
	}
}

func TestLoadDefaultsOpenAIReviewRoundsWhenUnsetOrEmpty(t *testing.T) {
	for _, value := range []string{"", "   "} {
		t.Run(strconv.Quote(value), func(t *testing.T) {
			t.Setenv("SLACK_SIGNING_SECRET", "secret")
			t.Setenv("GITLAB_TOKEN", "gitlab")
			t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
			t.Setenv("JIRA_EMAIL", "user@example.com")
			t.Setenv("JIRA_TOKEN", "jira")
			t.Setenv("OPENAI_API_KEY", "openai")
			t.Setenv("OPENAI_REVIEW_ROUNDS", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.OpenAIReviewRounds != 2 {
				t.Fatalf("OpenAIReviewRounds = %d, want 2", cfg.OpenAIReviewRounds)
			}
		})
	}
}

func TestLoadFailsForInvalidOpenAIReviewRounds(t *testing.T) {
	for _, value := range []string{"0", "3", "two"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SLACK_SIGNING_SECRET", "secret")
			t.Setenv("GITLAB_TOKEN", "gitlab")
			t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
			t.Setenv("JIRA_EMAIL", "user@example.com")
			t.Setenv("JIRA_TOKEN", "jira")
			t.Setenv("OPENAI_API_KEY", "openai")
			t.Setenv("OPENAI_REVIEW_ROUNDS", value)

			_, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want review rounds validation error")
			}
			if !strings.Contains(err.Error(), "OPENAI_REVIEW_ROUNDS") || !strings.Contains(err.Error(), "1 or 2") {
				t.Fatalf("Load() error = %v, want clear review rounds validation error", err)
			}
		})
	}
}

func TestLoadNormalisesConfiguredOpenAIReasoningEffort(t *testing.T) {
	t.Setenv("SLACK_SIGNING_SECRET", "secret")
	t.Setenv("GITLAB_TOKEN", "gitlab")
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	t.Setenv("JIRA_EMAIL", "user@example.com")
	t.Setenv("JIRA_TOKEN", "jira")
	t.Setenv("OPENAI_API_KEY", "openai")
	t.Setenv("OPENAI_REASONING_EFFORT", " XHIGH ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.OpenAIReasoningEffort != "xhigh" {
		t.Fatalf("OpenAIReasoningEffort = %q, want %q", cfg.OpenAIReasoningEffort, "xhigh")
	}
}

func TestLoadFailsForUnsupportedOpenAIReasoningEffort(t *testing.T) {
	t.Setenv("SLACK_SIGNING_SECRET", "secret")
	t.Setenv("GITLAB_TOKEN", "gitlab")
	t.Setenv("JIRA_BASE_URL", "https://example.atlassian.net")
	t.Setenv("JIRA_EMAIL", "user@example.com")
	t.Setenv("JIRA_TOKEN", "jira")
	t.Setenv("OPENAI_API_KEY", "openai")
	t.Setenv("OPENAI_REASONING_EFFORT", "maximum")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want reasoning effort validation error")
	}
	if !strings.Contains(err.Error(), "OPENAI_REASONING_EFFORT must be one of low, medium, high, or xhigh") {
		t.Fatalf("Load() error = %v, want clear reasoning effort validation error", err)
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
