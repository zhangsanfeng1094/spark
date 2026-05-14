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
	data, err := os.ReadFile(skillPath)
	if err != nil {
		if os.IsNotExist(err) {
			return SkillManifest{}, fmt.Errorf("skill package missing SKILL.md")
		}
		return SkillManifest{}, err
	}

	if manifest, err := parseFrontmatterManifest(string(data)); err == nil {
		return manifest, nil
	}

	if manifest, err := loadLegacySkillJSON(dir); err == nil {
		return manifest, nil
	}

	name, titleErr := readSkillTitle(skillPath)
	if titleErr != nil {
		return SkillManifest{}, titleErr
	}
	if strings.TrimSpace(name) == "" {
		return SkillManifest{}, fmt.Errorf("skill package missing YAML frontmatter with name and description")
	}
	return SkillManifest{}, fmt.Errorf("skill package missing YAML frontmatter with description for %s", name)
}

func parseFrontmatterManifest(content string) (SkillManifest, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return SkillManifest{}, fmt.Errorf("missing YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return SkillManifest{}, fmt.Errorf("unterminated YAML frontmatter")
	}

	fields := map[string]string{}
	for i := 1; i < end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch value {
		case "|", ">":
			block := make([]string, 0, 4)
			for i+1 < end {
				next := lines[i+1]
				if strings.TrimSpace(next) == "" {
					block = append(block, "")
					i++
					continue
				}
				if len(next) > 0 && next[0] != ' ' && next[0] != '\t' {
					break
				}
				block = append(block, strings.TrimSpace(next))
				i++
			}
			if value == ">" {
				fields[key] = strings.Join(block, " ")
			} else {
				fields[key] = strings.Join(block, "\n")
			}
		default:
			fields[key] = trimYAMLString(value)
		}
	}

	manifest := SkillManifest{
		Name:        normalizeManifestName(fields["name"], ""),
		Description: strings.TrimSpace(fields["description"]),
	}
	if manifest.Name == "" || manifest.Description == "" {
		return SkillManifest{}, fmt.Errorf("frontmatter requires name and description")
	}
	return manifest, nil
}

func loadLegacySkillJSON(dir string) (SkillManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "skill.json"))
	if err != nil {
		return SkillManifest{}, err
	}
	var manifest SkillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return SkillManifest{}, fmt.Errorf("parse skill.json: %w", err)
	}
	manifest.Name = normalizeManifestName(manifest.Name, "")
	manifest.Description = strings.TrimSpace(manifest.Description)
	if manifest.Name == "" || manifest.Description == "" {
		return SkillManifest{}, fmt.Errorf("legacy skill.json requires name and description")
	}
	return manifest, nil
}

func trimYAMLString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
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
