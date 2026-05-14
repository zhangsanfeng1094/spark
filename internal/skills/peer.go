package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func DefaultPeerRoot(peer string) (string, error) {
	return DefaultSkillRoot(ScopeGlobal, peer, "")
}

func Sync(opts SyncOptions) (SyncResult, error) {
	registry, err := LoadRegistry()
	if err != nil {
		return SyncResult{}, err
	}
	roots, err := DiscoverSkillRoots(opts.Scope, opts.ProjectRoot, opts.Targets, opts.OverrideRoot)
	if err != nil {
		return SyncResult{}, err
	}
	desired := make(map[string]*SkillEntry)
	rootIndex := make(map[string]SkillRoot, len(roots))
	for _, root := range roots {
		rootIndex[root.Path] = root
		if err := os.MkdirAll(root.Path, 0o755); err != nil {
			return SyncResult{}, err
		}
	}
	for _, entry := range registry.Skills {
		if entry == nil || !entry.Enabled || !entry.Managed || entry.InstalledPath == "" {
			continue
		}
		for _, root := range roots {
			if entry.Scope != root.Scope || !targetMatches(root.Target, entry.AgentTargets) {
				continue
			}
			desired[filepath.Join(root.Path, entry.Name)] = entry
		}
	}

	result := SyncResult{}
	for _, root := range roots {
		dirEntries, err := os.ReadDir(root.Path)
		if err != nil {
			return result, err
		}
		for _, dirEntry := range dirEntries {
			path := filepath.Join(root.Path, dirEntry.Name())
			if _, ok := desired[path]; ok {
				continue
			}
			managed, _ := managedProjectionState(path)
			if !managed {
				continue
			}
			if err := os.RemoveAll(path); err != nil {
				return result, err
			}
			result.Cleaned++
		}
	}

	paths := make([]string, 0, len(desired))
	for path := range desired {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		entry := desired[path]
		root := rootIndex[filepath.Dir(path)]
		state, marker := managedProjectionState(path)
		if !state {
			if _, err := os.Lstat(path); err == nil {
				result.Skipped++
				continue
			} else if !os.IsNotExist(err) {
				return result, err
			}
		}
		if state && projectionMatchesEntry(marker, entry, root.Scope, root.Target) {
			result.Skipped++
			continue
		}
		exists := state
		if !exists {
			if _, err := os.Lstat(path); err == nil {
				exists = true
			}
		}
		if state {
			if err := os.RemoveAll(path); err != nil {
				return result, err
			}
		}
		if err := materializeProjection(path, entry, root.Scope, root.Target); err != nil {
			return result, err
		}
		if exists {
			result.Updated++
		} else {
			result.Added++
		}
	}
	return result, nil
}

func SyncToPeer(peer, root string) error {
	_, err := Sync(SyncOptions{
		Scope:        ScopeGlobal,
		Targets:      []string{peer},
		OverrideRoot: root,
	})
	return err
}

func Import(opts ImportOptions) (ImportResult, error) {
	registry, err := LoadRegistry()
	if err != nil {
		return ImportResult{}, err
	}
	roots, err := DiscoverSkillRoots(opts.Scope, opts.ProjectRoot, opts.Targets, opts.OverrideRoot)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{}
	for _, root := range roots {
		entries, err := os.ReadDir(root.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return result, err
		}
		for _, entry := range entries {
			isDir := entry.IsDir()
			if !isDir {
				if info, err := os.Stat(filepath.Join(root.Path, entry.Name())); err == nil && info.IsDir() {
					isDir = true
				}
			}
			if !isDir {
				continue
			}
			name := NormalizeName(entry.Name())
			if _, exists := registry.Skills[name]; exists {
				result.Skipped++
				continue
			}
			sourcePath := filepath.Join(root.Path, entry.Name())
			manifest, err := LoadManifest(sourcePath)
			if err != nil {
				result.Invalid++
				continue
			}
			registry.Skills[name] = &SkillEntry{
				Name:                name,
				Scope:               root.Scope,
				AgentTargets:        []string{root.Target},
				SourceKind:          SourceKindImported,
				MaterializationMode: detectProjectionMode(sourcePath),
				SourceType:          SourceTypeLocal,
				Source:              sourcePath,
				Enabled:             true,
				Targets:             []string{root.Target},
				Managed:             false,
				InstalledPath:       sourcePath,
				Manifest:            manifest,
				InstalledAt:         time.Now().UTC(),
			}
			result.Added++
		}
	}
	if err := SaveRegistry(registry); err != nil {
		return result, err
	}
	return result, nil
}

func ImportFromPeer(peer, root string) (ImportResult, error) {
	return Import(ImportOptions{
		Scope:        ScopeGlobal,
		Targets:      []string{peer},
		OverrideRoot: root,
	})
}

func projectionMatchesEntry(marker *projectionMarker, entry *SkillEntry, scope, target string) bool {
	if marker == nil || entry == nil {
		return false
	}
	if marker.Mode == MaterializationSymlink {
		return filepath.Clean(marker.Source) == filepath.Clean(entry.InstalledPath)
	}
	return marker.Name == entry.Name &&
		marker.Scope == scope &&
		marker.Target == target &&
		filepath.Clean(marker.Source) == filepath.Clean(entry.InstalledPath) &&
		marker.Mode == entry.MaterializationMode
}

func materializeProjection(path string, entry *SkillEntry, scope, target string) error {
	switch entry.MaterializationMode {
	case MaterializationSymlink:
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.Symlink(entry.InstalledPath, path)
	default:
		if err := copyDir(entry.InstalledPath, path); err != nil {
			return err
		}
		return writeProjectionMarker(path, entry, scope, target)
	}
}

func detectProjectionMode(path string) string {
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return MaterializationSymlink
	}
	return MaterializationCopy
}

func targetsPeer(targets []string, peer string) bool {
	return targetMatches(peer, targets)
}

func scopeLabel(scope string) string {
	return strings.Title(NormalizeScope(scope))
}
