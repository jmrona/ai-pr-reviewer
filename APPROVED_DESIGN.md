# Approved Design: ai-pr-reviewer

## Goal

Build a greenfield, stateless Go service named `ai-pr-reviewer` at `/Users/jose.romero/Development/ai-pr-reviewer`. The service handles Slack `/review` slash commands, accepts one GitLab merge request URL and one Jira ticket URL in either order, immediately acknowledges the command, runs a multi-agent OpenAI review, and posts the result back through Slack `response_url`.

The project is an open-source repository that anyone can use. The MVP focuses on Slack, GitLab, Jira, and OpenAI. Future support for GitHub, Bitbucket, and optional ticket context is intentionally left as extension work.

## MVP Scope

- Slack slash command endpoint `/slack/review`.
- GitLab merge request diff fetching.
- Jira issue fetching and ADF text extraction.
- OpenAI multi-agent review using local markdown skill prompts.
- Slack `response_url` result posting.
- MIT `LICENSE`, `README.md`, `.env.example`, `.gitignore`, and Go module scaffolding.

## Future Extension Constraints

- Keep provider-specific URL classifiers in provider packages.
- Use orchestration interfaces for code changes, ticket context, reviewing, and Slack posting.
- Keep GitLab and Jira out of the agents package; agents receive formatted text.
- Keep ticket context as a string so optional ticket mode can later pass empty or synthetic context.
- Do not add plugin systems or provider registries for the MVP.

## Project Shape

- `cmd/server/main.go`
- `config/config.go`
- `internal/webhook/slack.go`
- `internal/review/orchestrator.go`
- `internal/slack/response.go`
- `internal/gitlab/client.go`
- `internal/jira/client.go`
- `internal/agents/reviewer.go`
- `internal/agents/loader.go`
- `skills/non_interactive.md`
- `skills/pragmatist.md`
- `skills/architect.md`
- `skills/designer.md`
- `skills/moderator.md`
- `LICENSE`
- `README.md`
- `.env.example`
- `.gitignore`
- `go.mod`

Use Go 1.22, the standard library HTTP server, and `github.com/sashabaranov/go-openai`. Do not use a database, persistence queue, web framework, or regex.

## Component Ownership

`cmd/server/main.go` owns process startup and dependency wiring.

`internal/webhook/slack.go` owns Slack slash-command HTTP concerns: signature validation, form parsing, URL role classification, immediate acknowledgement, and dispatching accepted requests to the review orchestrator.

`internal/review/orchestrator.go` owns the async review use case: fetch diff, fetch ticket, run reviewer, format Slack result, post success or safe failure, recover panics, and log major steps.

`internal/slack/response.go` owns Slack response_url posting and mrkdwn formatting.

`internal/gitlab/client.go` owns GitLab URL parsing, classification, API calls, and diff formatting.

`internal/jira/client.go` owns Jira URL parsing, classification, API calls, and ticket formatting.

`internal/agents/reviewer.go` owns OpenAI orchestration only.

`internal/agents/loader.go` owns project-local skill loading.

## Configuration

- `PORT`, default `8080`.
- `SLACK_BOT_TOKEN`, optional for MVP.
- `SLACK_SIGNING_SECRET`, required.
- `GITLAB_TOKEN`, required.
- `GITLAB_BASE_URL`, default `https://gitlab.com`.
- `JIRA_BASE_URL`, required.
- `JIRA_EMAIL`, required.
- `JIRA_TOKEN`, required.
- `OPENAI_API_KEY`, required.
- `OPENAI_MODEL`, default `gpt-4o`.

Timeout constants:

- Server read timeout: `5s`.
- Server write timeout: `10s`.
- External HTTP timeout: `20s`.
- Async review timeout: `10m`.

## Slack Command Behaviour

`/review` expects exactly two URL arguments in either order. The handler parses both with `net/url`, requires `https`, compares hostnames with `strings.EqualFold`, and rejects unless exactly one Jira ticket URL and one GitLab MR URL are found.

Slack signatures are verified with HMAC-SHA256 over `v0:{timestamp}:{raw_body}`. Timestamps older than 5 minutes or more than 5 minutes in the future are rejected.

Valid commands receive an immediate in-channel acknowledgement, then async processing posts the final result or a safe error through `response_url`.

## Non-Interactive Review Contract

All agent system prompts prepend `skills/non_interactive.md`. Agents must never ask Slack users questions, wait for approval, or defer the review. Missing information becomes assumptions, risks, or limitations. The moderator resolves disagreements into one final answer.

## Testing Strategy

Use Go tests with `testing`, `httptest`, and small fakes. Do not call live Slack, GitLab, Jira, or OpenAI. Cover config, signature validation, URL classification in both orders, provider parsing, diff truncation metadata, ADF extraction, skill loading, non-interactive prompt composition, moderator parsing, Slack formatting, orchestrator success/error paths, README presence, and key README sections.
