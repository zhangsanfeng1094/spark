package skills

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadManifest(dir string) (SkillManifest, error) {
	skillPath := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		if os.IsNotExist(err) {
			return SkillManifest{}, fmt.Errorf("skill package missing SKILL.md")
		}
		return SkillManifest{}, err
	}

	manifestPath := filepath.Join(dir, "skill.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var manifest SkillManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return SkillManifest{}, fmt.Errorf("parse skill.json: %w", err)
		}
		if strings.TrimSpace(manifest.Name) == "" {
			manifest.Name = NormalizeName(filepath.Base(dir))
		} else {
			manifest.Name = strings.ToLower(strings.TrimSpace(manifest.Name))
		}
		return manifest, nil
	} else if !os.IsNotExist(err) {
		return SkillManifest{}, err
	}

	name, err := readSkillTitle(skillPath)
	if err != nil {
		return SkillManifest{}, err
	}
	if name == "" {
		name = NormalizeName(filepath.Base(dir))
	}
	return SkillManifest{Name: name}, nil
}

func readSkillTitle(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if line != "" {
				return strings.ToLower(line), nil
			}
		}
	}
	return "", scanner.Err()
}
