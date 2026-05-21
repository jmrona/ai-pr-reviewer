# Production Readiness Review

Review the MR for production readiness using only the ticket context, MR diff, and previous agent analysis already in the prompt. State assumptions or limitations and continue from provided evidence.

Check:
- Requirements: acceptance criteria covered, implementation matches the ticket, no unexplained scope creep.
- Correctness: edge cases, unhappy paths, state transitions, data validation, and backwards compatibility where relevant.
- Security: authentication, authorisation, injection risks, secrets, sensitive data exposure, and safe defaults.
- Reliability: error handling, retries, idempotency, timeouts, partial failure behaviour, and race conditions.
- Performance: avoid obvious N+1 queries, unbounded loops, excessive allocations, missing indexes, or unnecessary network calls.
- Architecture: clear boundaries, consistent patterns, maintainable dependencies, and debuggable control flow.
- Tests: important logic covered, failure paths tested where meaningful, and tests assert behaviour rather than mocks only.
- Operations: migrations, feature flags, observability, documentation, and rollout risk when applicable.

Each issue should be actionable:
- Use `BLOCKER`, `WARNING`, or `SUGGESTION`.
- Include `file:line` when available.
- Explain what is wrong, why it matters, and the smallest credible fix.
- Keep wording concise and Slack friendly.

If evidence is missing, do not invent it. Put it in assumptions or limitations and avoid escalating severity beyond what the provided evidence supports.
