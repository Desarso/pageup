package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParsePageID(t *testing.T) {
	id := "019f620a-226d-7981-88d3-83da3b460b6c"
	endpoint := "https://pages.gabrielmalek.com"
	for _, value := range []string{
		id,
		endpoint + "/" + id,
		endpoint + "/" + id + "?preview=latest#top",
	} {
		parsed, err := parsePageID(value, endpoint)
		if err != nil {
			t.Fatalf("parsePageID(%q): %v", value, err)
		}
		if parsed != id {
			t.Fatalf("parsePageID(%q) = %q", value, parsed)
		}
	}

	for _, value := range []string{
		"not-a-page",
		"https://example.com/" + id,
		endpoint + "/" + id + "/extra",
		strings.ToUpper(id),
	} {
		if _, err := parsePageID(value, endpoint); err == nil {
			t.Fatalf("parsePageID(%q) unexpectedly succeeded", value)
		}
	}
}

func TestHelpExplainsUpdatesAndEmbeddedSkill(t *testing.T) {
	var output strings.Builder
	printUsage(&output)
	for _, expected := range []string{"pageup update URL", "pageup skill install", "same URL"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("help is missing %q", expected)
		}
	}
}
