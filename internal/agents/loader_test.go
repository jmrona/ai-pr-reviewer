package agents

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var projectSkillsDir = filepath.Clean(filepath.Join("..", "..", "skills"))

func TestLoadSkillsLoadsRequiredAndAdditionalSkills(t *testing.T) {
	dir := t.TempDir()
	for _, name := range append(RequiredSkills, "brainstorming-committee") {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(name), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}

	if skills["brainstorming-committee"] != "brainstorming-committee" {
		t.Fatal("additional skill was not loaded")
	}
}

func TestLoadSkillsLoadsAllProjectSkillFiles(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(projectSkillsDir, "*.md"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Glob() matched no project skills")
	}

	skills, err := LoadSkills(projectSkillsDir)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}

	for _, file := range files {
		name := strings.TrimSuffix(path.Base(file), ".md")
		if strings.TrimSpace(skills[name]) == "" {
			t.Fatalf("skill %q was not loaded from %s", name, file)
		}
	}
}

func TestLoadSkillsCachesCopiedExtrasByFilenameWithoutExtension(t *testing.T) {
	skills, err := LoadSkills(projectSkillsDir)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}

	for _, name := range []string{"committee_perspectives", "production_readiness", "severity_discipline"} {
		if strings.TrimSpace(skills[name]) == "" {
			t.Fatalf("skill %q was not cached by extensionless filename key", name)
		}
		if _, ok := skills[name+".md"]; ok {
			t.Fatalf("skill %q was cached with .md extension", name)
		}
	}
}

func TestLoadSkillsFailsWhenRequiredSkillIsMissing(t *testing.T) {
	_, err := LoadSkills(t.TempDir())
	if err == nil {
		t.Fatal("LoadSkills() error = nil, want error")
	}
}

func TestComposeSystemPromptPrependsNonInteractiveSkill(t *testing.T) {
	skills := map[string]string{
		"non_interactive": "do not require input",
		"pragmatist":      "review behaviour",
		"architect":       "review architecture",
		"designer":        "review usability",
		"moderator":       "summarise findings",
	}

	for _, role := range []string{"pragmatist", "architect", "designer", "moderator"} {
		prompt, err := ComposeSystemPrompt(skills, role)
		if err != nil {
			t.Fatalf("ComposeSystemPrompt(%q) error = %v", role, err)
		}

		if !strings.HasPrefix(prompt, "do not require input\n\n"+skills[role]) {
			t.Fatalf("prompt for %q = %q, want non-interactive prefix before role prompt", role, prompt)
		}
	}
}

func TestProjectSkillFilesDoNotContainProhibitedPromptPhrases(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(projectSkillsDir, "*.md"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Glob() matched no project skills")
	}

	prohibitedPhrases := []string{
		"approval",
		"plan mode",
		"dispatch",
		"subagent",
		"request input",
		"requests for input",
		"request follow-up",
		"request more information",
		"run command",
		"read file",
		"opencode",
		"fut" + "ure " + "publishing",
		"fut" + "ure " + "plc",
		"fut" + "ure" + "publishing",
		"fut" + "ure" + "net",
		"purch" + "1",
		"em" + "ber",
		"em" + "ber-",
		"em" + "ber/" + "em" + "ber",
		"fut" + "ure" + "plc",
	}
	prohibitedWordPatterns := map[string]*regexp.Regexp{
		"ask":       regexp.MustCompile(`(?i)\bask\b`),
		"question":  regexp.MustCompile(`(?i)\bquestion\b`),
		"questions": regexp.MustCompile(`(?i)\bquestions\b`),
		"request":   regexp.MustCompile(`(?i)\brequest\b`),
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", file, err)
		}

		content := strings.ToLower(string(data))
		for _, phrase := range prohibitedPhrases {
			if strings.Contains(content, phrase) {
				t.Fatalf("%s contains prohibited phrase %q", file, phrase)
			}
		}

		for phrase, pattern := range prohibitedWordPatterns {
			if pattern.Match(data) {
				t.Fatalf("%s contains prohibited phrase %q", file, phrase)
			}
		}
	}
}
