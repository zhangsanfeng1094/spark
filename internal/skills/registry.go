package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func sparkDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".spark"), nil
}

func RegistryPath() (string, error) {
	dir, err := sparkDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skill-registry.json"), nil
}

func StorageRoot() (string, error) {
	dir, err := sparkDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills"), nil
}

func CatalogRoot() (string, error) {
	dir, err := sparkDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skill-catalogs"), nil
}

func DefaultRegistry() *Registry {
	return &Registry{
		Version: CurrentRegistryVersion,
		Skills:  map[string]*SkillEntry{},
	}
}

func LoadRegistry() (*Registry, error) {
	path, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultRegistry(), nil
		}
		return nil, err
	}

	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("parse skill registry: %w", err)
	}
	NormalizeRegistry(&registry)
	return &registry, nil
}

func SaveRegistry(registry *Registry) error {
	NormalizeRegistry(registry)
	path, err := RegistryPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return writeWithBackup(path, data)
}

func NormalizeRegistry(registry *Registry) {
	if registry == nil {
		return
	}
	if registry.Version == 0 {
		registry.Version = CurrentRegistryVersion
	}
	if registry.Skills == nil {
		registry.Skills = map[string]*SkillEntry{}
	}

	normalized := make(map[string]*SkillEntry, len(registry.Skills))
	for name, entry := range registry.Skills {
		if entry == nil {
			continue
		}
		key := NormalizeName(firstNonEmpty(entry.Name, name))
		entry.Name = key
		entry.SourceType = NormalizeSourceType(entry.SourceType)
		entry.Targets = NormalizeTargets(entry.Targets)
		entry.Manifest.Name = normalizeManifestName(entry.Manifest.Name, key)
		normalized[key] = entry
	}
	registry.Skills = normalized
}

func NormalizeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	return strings.ReplaceAll(name, "_", "-")
}

func NormalizeTargets(targets []string) []string {
	if len(targets) == 0 {
		return []string{"codex", "claude"}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		normalized := NormalizePeer(target)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return []string{"codex", "claude"}
	}
	slices.Sort(out)
	return out
}

func NormalizePeer(peer string) string {
	switch strings.ToLower(strings.TrimSpace(peer)) {
	case "codex":
		return "codex"
	case "claude":
		return "claude"
	default:
		return ""
	}
}

func NormalizeSourceType(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case SourceTypeGit:
		return SourceTypeGit
	case "", SourceTypeLocal:
		return SourceTypeLocal
	default:
		return strings.ToLower(strings.TrimSpace(sourceType))
	}
}

func normalizeManifestName(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return strings.TrimSpace(strings.ToLower(name))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
