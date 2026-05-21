# ai-pr-reviewer

`ai-pr-reviewer` is a stateless Slack slash-command service that runs an AI-assisted merge request review. A developer runs `/review` with a GitLab merge request URL and a Jira ticket URL, and the service fetches both contexts, runs a multi-agent OpenAI review, and posts the result back to Slack.

## Why

Code reviews often need both implementation context and ticket intent. This service combines the merge request diff with the ticket description, then asks specialised AI reviewers to check behaviour, architecture, security, performance, readability, and tests.

## Features

- Slack `/review` slash command endpoint.
- Slack signature validation and replay protection.
- GitLab merge request diff fetching.
- Jira issue fetching with Atlassian Document Format text extraction.
- Multi-agent OpenAI review using local markdown skill prompts.
- Non-interactive agent contract for Slack workflows.
- Slack `response_url` result posting.
- Stateless operation with no database or queue.

## Architecture

```text
Slack /review
  -> internal/webhook validates and classifies URLs
  -> internal/review orchestrates the async review
  -> internal/gitlab fetches and formats MR diff
  -> internal/jira fetches and formats ticket context
  -> internal/agents runs OpenAI reviewer rounds
  -> internal/slack formats and posts the result
```

## Prerequisites

- Go 1.22 or newer.
- A Slack app with a slash command.
- A GitLab token that can read merge requests.
- Jira API credentials.
- An OpenAI API key.

## Configuration

All configuration is read from environment variables.

| Variable | Required | Default | Description |
|---|---:|---|---|
| `PORT` | No | `8080` | HTTP server port. |
| `SLACK_SIGNING_SECRET` | Yes | | Slack signing secret for request verification. |
| `SLACK_BOT_TOKEN` | No | | Reserved for future direct Slack Web API posting. |
| `GITLAB_TOKEN` | Yes | | Token used for GitLab API requests. |
| `GITLAB_BASE_URL` | No | `https://gitlab.com` | GitLab instance base URL. |
| `JIRA_BASE_URL` | Yes | | Jira instance base URL. |
| `JIRA_EMAIL` | Yes | | Jira account email for basic auth. |
| `JIRA_TOKEN` | Yes | | Jira API token. |
| `OPENAI_API_KEY` | Yes | | OpenAI API key. |
| `OPENAI_MODEL` | No | `gpt-4o` | OpenAI model used for every agent round. |

## Slack App Setup

Create a Slack slash command named `/review` and set the request URL to:

```text
https://your-service-host/slack/review
```

Slack sends command text in the request body. The service expects one GitLab merge request URL and one Jira ticket URL in either order.

## Local Development

Copy the example environment file and set real values:

```sh
cp .env.example .env
```

Run the service:

```sh
go run ./cmd/server
```

Health check:

```sh
curl http://localhost:8080/health
```

## Docker

Build the image:

```sh
docker build -t ai-pr-reviewer .
```

Run the container locally:

```sh
docker run --rm -p 8080:8080 --env-file .env ai-pr-reviewer
```

Check the service health:

```sh
curl http://localhost:8080/health
```

Deployment platforms should provide secrets as environment variables. Set `PORT` if the platform requires the service to listen on a specific port.

## Example Commands

Jira first:

```text
/review https://jira.example.com/browse/PROJ-141 https://gitlab.example.com/platform/application/-/merge_requests/108
```

GitLab first:

```text
/review https://gitlab.example.com/platform/application/-/merge_requests/108 https://jira.example.com/browse/PROJ-141
```

## Testing

Run all tests:

```sh
go test ./...
```

Tests use local fakes and `httptest`. They do not call live Slack, GitLab, Jira, or OpenAI.

## Security Notes

- Slack requests are verified with `X-Slack-Signature` and `X-Slack-Request-Timestamp`.
- Requests older than 5 minutes or more than 5 minutes in the future are rejected.
- Secrets are read from environment variables only.
- Error messages posted to Slack avoid tokens, auth headers, and full third-party response bodies.

## Current Limitations

- Only GitLab merge requests are supported.
- Only Jira tickets are supported.
- The ticket URL is required in the MVP.
- Reviews use a strict markdown moderator format rather than JSON output.
- In-flight reviews can be lost if the process exits because the service is intentionally stateless.

## Roadmap

- GitHub pull request support.
- Bitbucket pull request support.
- Optional ticket context for teams that do not use Jira.
- Stricter structured AI output.
- Direct Slack Web API posting when `response_url` is not suitable.

## Licence

This project is released under the MIT Licence. See `LICENSE`.
