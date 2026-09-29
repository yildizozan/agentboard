package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func installSkills(cmd *cobra.Command, scope, dir string) error {
	if len(skillContent) == 0 {
		return fmt.Errorf("agentboard skill is missing from the executable")
	}
	if scope == "user" {
		var err error
		dir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory: %w", err)
		}
	}
	for _, client := range []struct {
		name string
		path string
	}{
		{"Codex", filepath.Join(dir, ".agents", "skills", "agentboard", "SKILL.md")},
		{"Claude Code", filepath.Join(dir, ".claude", "skills", "agentboard", "SKILL.md")},
	} {
		if err := writeSkill(client.path); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Installed agentboard skill for %s (%s): %s\n", client.name, scope, client.path)
	}
	return nil
}

func writeSkill(path string) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, skillContent) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	for _, candidate := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(candidate)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to replace skill through symlink %s", candidate)
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", candidate, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create skill directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, skillContent, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
