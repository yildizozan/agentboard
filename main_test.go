package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAgentboardSkillIsEmbedded(t *testing.T) {
	source, err := os.ReadFile("skill/agentboard/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(agentboardSkill) == 0 || !bytes.Equal(agentboardSkill, source) {
		t.Error("embedded skill differs from the repository skill")
	}
}

func TestBuiltBinaryInstallsProjectSkills(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agentboard")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agentboard: %v\n%s", err, output)
	}
	project := t.TempDir()
	install := exec.Command(binary, "install", "--scope", "project")
	install.Dir = project
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install from built binary: %v\n%s", err, output)
	}
	for _, client := range []string{".agents", ".claude"} {
		path := filepath.Join(project, client, "skills", "agentboard", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, agentboardSkill) {
			t.Errorf("installed %s skill differs from binary", client)
		}
	}
}
