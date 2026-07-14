package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSkillRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex-home"))
	t.Setenv("PAGEUP_SKILLS_DIR", "")

	root, harness, err := resolveSkillRoot("auto", "")
	if err != nil {
		t.Fatal(err)
	}
	if harness != "codex" || root != filepath.Join(home, "codex-home", "skills") {
		t.Fatalf("auto root = %q, harness = %q", root, harness)
	}

	custom, harness, err := resolveSkillRoot("auto", filepath.Join(home, "custom-skills"))
	if err != nil {
		t.Fatal(err)
	}
	if harness != "custom" || custom != filepath.Join(home, "custom-skills") {
		t.Fatalf("custom root = %q, harness = %q", custom, harness)
	}

	if _, _, err := resolveSkillRoot("codex", filepath.Join(home, "custom-skills")); err == nil {
		t.Fatal("combined harness and target unexpectedly succeeded")
	}
}

func TestResolveProjectSkillRoot(t *testing.T) {
	workingDirectory := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(previous) })

	root, harness, err := resolveSkillRoot("project", "")
	if err != nil {
		t.Fatal(err)
	}
	if harness != "project" || root != filepath.Join(workingDirectory, ".agents", "skills") {
		t.Fatalf("project root = %q, harness = %q", root, harness)
	}
}
