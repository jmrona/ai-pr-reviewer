package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPort           = "8080"
	DefaultGitLabBaseURL  = "https://gitlab.com"
	DefaultOpenAIModel    = "gpt-4o"
	DefaultReviewRounds   = 2
	DefaultReviewTraceDir = ".review-traces"

	ServerReadTimeout   = 5 * time.Second
	ServerWriteTimeout  = 10 * time.Second
	ExternalHTTPTimeout = 20 * time.Second
	ReviewTimeout       = 10 * time.Minute
)

type Config struct {
	Port                      string
	SlackBotToken             string
	SlackSigningSecret        string
	GitLabToken               string
	GitLabBaseURL             string
	JiraBaseURL               string
	JiraEmail                 string
	JiraToken                 string
	OpenAIAPIKey              string
	OpenAIModel               string
	OpenAIReasoningEffort     string
	OpenAIReviewRounds        int
	ReviewTraceEnabled        bool
	ReviewTraceDir            string
	ReviewTraceIncludePrompts bool
}

func Load() (Config, error) {
	reviewRounds, err := parseOpenAIReviewRounds(os.Getenv("OPENAI_REVIEW_ROUNDS"))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:                      valueOrDefault("PORT", DefaultPort),
		SlackBotToken:             os.Getenv("SLACK_BOT_TOKEN"),
		SlackSigningSecret:        os.Getenv("SLACK_SIGNING_SECRET"),
		GitLabToken:               os.Getenv("GITLAB_TOKEN"),
		GitLabBaseURL:             valueOrDefault("GITLAB_BASE_URL", DefaultGitLabBaseURL),
		JiraBaseURL:               os.Getenv("JIRA_BASE_URL"),
		JiraEmail:                 os.Getenv("JIRA_EMAIL"),
		JiraToken:                 os.Getenv("JIRA_TOKEN"),
		OpenAIAPIKey:              os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:               valueOrDefault("OPENAI_MODEL", DefaultOpenAIModel),
		OpenAIReasoningEffort:     normaliseOpenAIReasoningEffort(os.Getenv("OPENAI_REASONING_EFFORT")),
		OpenAIReviewRounds:        reviewRounds,
		ReviewTraceEnabled:        parseBool("REVIEW_TRACE_ENABLED"),
		ReviewTraceDir:            valueOrDefault("REVIEW_TRACE_DIR", DefaultReviewTraceDir),
		ReviewTraceIncludePrompts: parseBool("REVIEW_TRACE_INCLUDE_PROMPTS"),
	}

	var missing []string
	for name, value := range map[string]string{
		"SLACK_SIGNING_SECRET": cfg.SlackSigningSecret,
		"GITLAB_TOKEN":         cfg.GitLabToken,
		"JIRA_BASE_URL":        cfg.JiraBaseURL,
		"JIRA_EMAIL":           cfg.JiraEmail,
		"JIRA_TOKEN":           cfg.JiraToken,
		"OPENAI_API_KEY":       cfg.OpenAIAPIKey,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if !isValidOpenAIReasoningEffort(cfg.OpenAIReasoningEffort) {
		return Config{}, errors.New("OPENAI_REASONING_EFFORT must be one of low, medium, high, or xhigh")
	}

	return cfg, nil
}

func MustLoad() Config {
	cfg, err := Load()
	if err != nil {
		panic(err)
	}
	return cfg
}

func valueOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func parseBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func normaliseOpenAIReasoningEffort(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isValidOpenAIReasoningEffort(value string) bool {
	switch value {
	case "", "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func parseOpenAIReviewRounds(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultReviewRounds, nil
	}
	rounds, err := strconv.Atoi(trimmed)
	if err != nil || !isValidReviewRounds(rounds) {
		return 0, errors.New("OPENAI_REVIEW_ROUNDS must be 1 or 2")
	}
	return rounds, nil
}

func isValidReviewRounds(rounds int) bool {
	return rounds == 1 || rounds == 2
}

func IsMissingConfigError(err error) bool {
	return err != nil && !errors.Is(err, nil) && strings.Contains(err.Error(), "missing required environment variables")
}
