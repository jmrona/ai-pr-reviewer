# Severity Discipline

Use severity to describe merge risk, not personal preference.

## BLOCKER

Use when the MR should not merge as-is:
- Broken ticket requirement or acceptance criterion.
- Data loss, security vulnerability, privilege bypass, or serious privacy exposure.
- Production crash, migration failure, or unrecoverable operational risk.
- Incorrect behaviour with likely user impact.
- Missing test coverage for a high-risk or newly critical path.

## WARNING

Use when the MR can be understood but should be fixed before merge or soon after:
- Meaningful edge case, reliability, performance, or maintainability risk.
- Incomplete error handling or validation with plausible impact.
- Architectural drift that increases future change cost.
- Test gap for non-critical but important behaviour.

## SUGGESTION

Use for improvements that do not block safe progress:
- Naming, readability, small refactors, documentation polish, or minor optimisation.
- Alternative implementation that is clearer but not materially safer.
- Non-critical test or observability improvement.

## Calibration Rules

- Do not inflate severity for style preferences.
- Do not downgrade correctness, security, data integrity, or production safety issues.
- Prefer one precise finding over several overlapping ones.
- Deduplicate repeated symptoms under the strongest root cause.
- If confidence is limited by missing evidence, say so in assumptions or limitations.
- Do not require Slack user input; state the assumption used for the verdict.
