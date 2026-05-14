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
		entry.Scope = NormalizeScope(entry.Scope)
		entry.SourceType = NormalizeSourceType(entry.SourceType)
		entry.SourceKind = NormalizeSourceKind(entry.SourceKind, entry.SourceType, entry.Managed)
		entry.AgentTargets = NormalizeTargets(firstNonEmptySlice(entry.AgentTargets, entry.Targets))
		entry.Targets = slices.Clone(entry.AgentTargets)
		entry.MaterializationMode = NormalizeMaterializationMode(entry.MaterializationMode)
		entry.Manifest.Name = normalizeManifestName(entry.Manifest.Name, key)
		entry.Manifest.Description = strings.TrimSpace(entry.Manifest.Description)
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
		return []string{"agents", "claude", "codex"}
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
		return []string{"agents", "claude", "codex"}
	}
	slices.Sort(out)
	return out
}

func NormalizePeer(peer string) string {
	switch strings.ToLower(strings.TrimSpace(peer)) {
	case "agents", "agent":
		return "agents"
	case "codex":
		return "codex"
	case "claude":
		return "claude"
	default:
		return ""
	}
}

func NormalizeScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case ScopeProject:
		return ScopeProject
	case "", ScopeGlobal:
		return ScopeGlobal
	default:
		return strings.ToLower(strings.TrimSpace(scope))
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

func NormalizeSourceKind(sourceKind, sourceType string, managed bool) string {
	switch strings.ToLower(strings.TrimSpace(sourceKind)) {
	case SourceKindLocal, SourceKindGit, SourceKindCatalog, SourceKindImported:
		return strings.ToLower(strings.TrimSpace(sourceKind))
	}
	switch NormalizeSourceType(sourceType) {
	case SourceTypeGit:
		return SourceKindGit
	case SourceTypeLocal:
		if !managed {
			return SourceKindImported
		}
		return SourceKindLocal
	default:
		if !managed {
			return SourceKindImported
		}
		return SourceKindLocal
	}
}

func NormalizeMaterializationMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case MaterializationSymlink:
		return MaterializationSymlink
	case "", MaterializationCopy:
		return MaterializationCopy
	default:
		return strings.ToLower(strings.TrimSpace(mode))
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

func firstNonEmptySlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}
