package skills

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func Install(opts InstallOptions) (*SkillEntry, error) {
	registry, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	name := NormalizeName(opts.Name)
	if name == "" {
		name = NormalizeName(filepath.Base(strings.TrimRight(opts.Source, string(filepath.Separator))))
	}
	if name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	sourceType := NormalizeSourceType(opts.SourceType)
	if strings.TrimSpace(opts.Source) == "" {
		return nil, fmt.Errorf("skill source is required")
	}

	storeRoot, err := StorageRoot()
	if err != nil {
		return nil, err
	}
	targetDir := filepath.Join(storeRoot, name)

	if err := materializeSource(sourceType, opts.Source, opts.Ref, opts.Subdir, targetDir); err != nil {
		return nil, err
	}
	manifest, err := LoadManifest(targetDir)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = normalizeManifestName(manifest.Name, name)
	}

	entry := &SkillEntry{
		Name:                name,
		Scope:               NormalizeScope(opts.Scope),
		AgentTargets:        NormalizeTargets(opts.Targets),
		SourceKind:          NormalizeSourceKind(opts.SourceKind, sourceType, true),
		MaterializationMode: NormalizeMaterializationMode(opts.MaterializationMode),
		SourceType:          sourceType,
		Source:              opts.Source,
		Ref:                 strings.TrimSpace(opts.Ref),
		Subdir:              strings.TrimSpace(opts.Subdir),
		Enabled:             true,
		Targets:             NormalizeTargets(opts.Targets),
		Managed:             true,
		InstalledPath:       targetDir,
		Manifest:            manifest,
		InstalledAt:         time.Now().UTC(),
	}
	registry.Skills[name] = entry
	if err := SaveRegistry(registry); err != nil {
		return nil, err
	}
	return entry, nil
}

func SetEnabled(name string, enabled bool) error {
	registry, err := LoadRegistry()
	if err != nil {
		return err
	}
	entry := registry.Skills[NormalizeName(name)]
	if entry == nil {
		return fmt.Errorf("skill not found: %s", name)
	}
	entry.Enabled = enabled
	return SaveRegistry(registry)
}

func Remove(name string) error {
	registry, err := LoadRegistry()
	if err != nil {
		return err
	}
	key := NormalizeName(name)
	entry := registry.Skills[key]
	if entry == nil {
		return fmt.Errorf("skill not found: %s", name)
	}
	if entry.Managed && entry.InstalledPath != "" {
		if err := os.RemoveAll(entry.InstalledPath); err != nil {
			return err
		}
	}
	delete(registry.Skills, key)
	return SaveRegistry(registry)
}

func materializeSource(sourceType, source, ref, subdir, targetDir string) error {
	switch sourceType {
	case SourceTypeGit:
		return installFromGit(source, ref, subdir, targetDir)
	case SourceTypeLocal:
		sourceDir := source
		if subdir != "" {
			sourceDir = filepath.Join(sourceDir, subdir)
		}
		return copyDir(sourceDir, targetDir)
	default:
		return fmt.Errorf("unsupported skill source type: %s", sourceType)
	}
}

func installFromGit(source, ref, subdir, targetDir string) error {
	tmpDir, err := os.MkdirTemp("", "spark-skill-git-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command("git", "clone", "--depth", "1", source, tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(ref) != "" {
		checkout := exec.Command("git", "-C", tmpDir, "checkout", ref)
		if output, err := checkout.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}

	sourceDir := tmpDir
	if subdir != "" {
		candidate := filepath.Join(tmpDir, subdir)
		if hasSkillMarkdown(candidate) {
			sourceDir = candidate
		} else if hasSkillMarkdown(tmpDir) {
			sourceDir = tmpDir
		} else {
			sourceDir = candidate
		}
	}
	return copyDir(sourceDir, targetDir)
}

func hasSkillMarkdown(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}
