package agents

import "testing"

func TestParseModeratorOutputParsesSectionsAndLocations(t *testing.T) {
	output := `TICKET_COVERAGE:
:white_check_mark: All criteria covered

BLOCKERS:
- [Architect] SQL injection risk (api/user.go:42)

WARNINGS:
- [Pragmatist] Missing empty state (collab/page.tsx)

SUGGESTIONS:
- [Designer] Rename getData for clarity

ASSUMPTIONS:
- Acceptance criteria are in the Jira description

SUMMARY:
Fix the blocker before merging.`

	result, err := ParseModeratorOutput(output)
	if err != nil {
		t.Fatalf("ParseModeratorOutput() error = %v", err)
	}

	if result.Blockers[0].File != "api/user.go" || result.Blockers[0].Line != 42 {
		t.Fatalf("blocker location = %s:%d", result.Blockers[0].File, result.Blockers[0].Line)
	}
	if result.Warnings[0].File != "collab/page.tsx" || result.Warnings[0].Line != 0 {
		t.Fatalf("warning location = %s:%d", result.Warnings[0].File, result.Warnings[0].Line)
	}
	if len(result.Assumptions) != 1 {
		t.Fatalf("Assumptions length = %d, want 1", len(result.Assumptions))
	}
}

func TestParseModeratorOutputHandlesNoneSections(t *testing.T) {
	output := `TICKET_COVERAGE:
:warning: Partially covered

BLOCKERS:
None

WARNINGS:
None

SUGGESTIONS:
None

ASSUMPTIONS:
None

SUMMARY:
No findings.`

	result, err := ParseModeratorOutput(output)
	if err != nil {
		t.Fatalf("ParseModeratorOutput() error = %v", err)
	}
	if len(result.Blockers) != 0 || len(result.Warnings) != 0 || len(result.Suggestions) != 0 || len(result.Assumptions) != 0 {
		t.Fatalf("expected empty sections, got %#v", result)
	}
}
