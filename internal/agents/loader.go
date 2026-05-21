package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var RequiredSkills = []string{"non_interactive", "pragmatist", "architect", "designer", "moderator"}

func LoadSkills(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills directory: %w", err)
	}

	skills := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading skill %s: %w", entry.Name(), err)
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		skills[name] = string(data)
	}

	for _, name := range RequiredSkills {
		if strings.TrimSpace(skills[name]) == "" {
			return nil, fmt.Errorf("required skill %q is missing", name)
		}
	}

	return skills, nil
}

func ComposeSystemPrompt(skills map[string]string, role string) (string, error) {
	nonInteractive := strings.TrimSpace(skills["non_interactive"])
	rolePrompt := strings.TrimSpace(skills[role])
	if nonInteractive == "" {
		return "", fmt.Errorf("required skill %q is missing", "non_interactive")
	}
	if rolePrompt == "" {
		return "", fmt.Errorf("required skill %q is missing", role)
	}
	return nonInteractive + "\n\n" + rolePrompt, nil
}
