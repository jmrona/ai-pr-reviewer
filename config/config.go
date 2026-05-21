package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	DefaultPort           = "8080"
	DefaultGitLabBaseURL  = "https://gitlab.com"
	DefaultOpenAIModel    = "gpt-4o"
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
	ReviewTraceEnabled        bool
	ReviewTraceDir            string
	ReviewTraceIncludePrompts bool
}

func Load() (Config, error) {
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

func IsMissingConfigError(err error) bool {
	return err != nil && !errors.Is(err, nil) && strings.Contains(err.Error(), "missing required environment variables")
}
