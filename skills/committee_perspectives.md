# Committee Perspectives

Use three complementary perspectives when reviewing an MR. Each perspective should work only from the ticket context, MR diff, and previous agent analysis already provided in the prompt.

## Pragmatist

Prioritise:
- Whether the MR delivers the ticket and acceptance criteria.
- Simplicity, low maintenance cost, and minimal moving parts.
- Using existing patterns instead of adding new abstractions.
- Practical edge cases that affect real users or operators.

Be sceptical of:
- Scope creep beyond the ticket.
- Future-proofing that adds complexity now.
- Unclear behaviour hidden behind clever code.

## Architect

Prioritise:
- Security, data integrity, and production safety.
- Clear boundaries, separation of concerns, and consistency with existing patterns.
- Performance, scalability, concurrency, and error handling risks.
- Designs that remain debuggable and testable.

Be sceptical of:
- Bypassing established architecture.
- Tight coupling or hidden shared state.
- Changes that make future fixes disproportionately expensive.

## Advocate

Prioritise:
- Correctness under edge cases and failure modes.
- User and developer experience.
- Clear validation, clear errors, and safe defaults.
- Tests that prove the important behaviour.

Be sceptical of:
- Happy-path-only implementations.
- Silent failures or confusing outcomes.
- APIs and behaviours that are easy to misuse.

## Conflict Resolution

When perspectives disagree:
- Identify the actual trade-off, not just the preferred solution.
- Prefer a compromise only when it addresses the highest-severity risks without adding unnecessary complexity.
- If there is no compromise, choose the option that best satisfies the ticket while protecting production safety.
- State accepted trade-offs as assumptions or limitations; do not require input from Slack users.
