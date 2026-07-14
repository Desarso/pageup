package pageskill

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const Name = "pages"

//go:embed pages/SKILL.md pages/agents/openai.yaml
var content embed.FS

var embeddedFiles = []string{
	"SKILL.md",
	filepath.Join("agents", "openai.yaml"),
}

func SkillMarkdown() ([]byte, error) {
	return content.ReadFile("pages/SKILL.md")
}

func Install(skillsRoot string, force bool) (string, error) {
	if skillsRoot == "" {
		return "", errors.New("skills root cannot be empty")
	}
	destination := filepath.Join(skillsRoot, Name)
	if info, err := os.Stat(destination); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("skill destination exists and is not a directory: %s", destination)
		}
		if !force {
			return "", fmt.Errorf("skill already exists at %s (use --force to replace embedded files)", destination)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	for _, name := range embeddedFiles {
		data, err := content.ReadFile(filepath.ToSlash(filepath.Join("pages", name)))
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", name, err)
		}
		path := filepath.Join(destination, name)
		if err := writeAtomic(path, data); err != nil {
			return "", fmt.Errorf("install %s: %w", name, err)
		}
	}
	return destination, nil
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".pageup-skill-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return err
		}
		if retryErr := os.Rename(temporaryName, path); retryErr != nil {
			return retryErr
		}
	}
	return os.Chmod(path, 0o644)
}
