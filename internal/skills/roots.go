package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const projectionMarkerName = ".spark-skill.json"

type projectionMarker struct {
	Name   string `json:"name"`
	Scope  string `json:"scope"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
	Source string `json:"source"`
}

func ResolveProjectRoot(projectRoot string) (string, error) {
	if strings.TrimSpace(projectRoot) != "" {
		return filepath.Abs(projectRoot)
	}
	return os.Getwd()
}

func DefaultSkillRoot(scope, target, projectRoot string) (string, error) {
	target = NormalizePeer(target)
	if target == "" {
		return "", fmt.Errorf("unsupported skill target: %s", target)
	}
	switch NormalizeScope(scope) {
	case ScopeProject:
		root, err := ResolveProjectRoot(projectRoot)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, scopeDirName(target), "skills"), nil
	case ScopeGlobal:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, scopeDirName(target), "skills"), nil
	default:
		return "", fmt.Errorf("unsupported skill scope: %s", scope)
	}
}

func DiscoverSkillRoots(scope, projectRoot string, targets []string, overrideRoot string) ([]SkillRoot, error) {
	resolvedTargets := NormalizeTargets(targets)
	scopes := []string{ScopeProject, ScopeGlobal}
	if normalizedScope := NormalizeScope(scope); normalizedScope != "" && scope != "" {
		scopes = []string{normalizedScope}
	}
	roots := make([]SkillRoot, 0, len(scopes)*len(resolvedTargets))
	for _, rootScope := range scopes {
		for _, target := range resolvedTargets {
			rootPath := overrideRoot
			if rootPath == "" {
				var err error
				rootPath, err = DefaultSkillRoot(rootScope, target, projectRoot)
				if err != nil {
					return nil, err
				}
			}
			roots = append(roots, SkillRoot{Scope: rootScope, Target: target, Path: rootPath})
			if overrideRoot != "" {
				return roots, nil
			}
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].Scope != roots[j].Scope {
			return roots[i].Scope == ScopeProject
		}
		return roots[i].Target < roots[j].Target
	})
	return roots, nil
}

func ProjectionStatuses(entry *SkillEntry, projectRoot string) ([]ProjectionStatus, error) {
	if entry == nil {
		return nil, nil
	}
	targets := entry.AgentTargets
	if len(targets) == 0 {
		targets = entry.Targets
	}
	roots, err := DiscoverSkillRoots(entry.Scope, projectRoot, targets, "")
	if err != nil {
		return nil, err
	}
	statuses := make([]ProjectionStatus, 0, len(roots))
	for _, root := range roots {
		path := filepath.Join(root.Path, entry.Name)
		state := "missing"
		info, err := readManagedProjection(path)
		if err == nil {
			if info != nil && projectionMarkerMatchesStatus(info, entry, root.Scope, root.Target) {
				state = "managed"
			} else {
				state = "drift"
			}
		} else if os.IsNotExist(err) {
			state = "missing"
		} else {
			state = "drift"
		}
		statuses = append(statuses, ProjectionStatus{
			Scope:  root.Scope,
			Target: root.Target,
			Path:   path,
			State:  state,
		})
	}
	return statuses, nil
}

func projectionMarkerMatchesStatus(marker *projectionMarker, entry *SkillEntry, scope, target string) bool {
	if marker == nil || entry == nil {
		return false
	}
	if marker.Mode == MaterializationSymlink {
		return filepath.Clean(marker.Source) == filepath.Clean(entry.InstalledPath)
	}
	return marker.Name == entry.Name && marker.Scope == scope && marker.Target == target
}

func scopeDirName(target string) string {
	switch NormalizePeer(target) {
	case "agents":
		return ".agents"
	case "claude":
		return ".claude"
	default:
		return ".codex"
	}
}

func readManagedProjection(path string) (*projectionMarker, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(path)
		if err != nil {
			return nil, err
		}
		absTarget, err := filepath.Abs(linkTarget)
		if err != nil {
			return nil, err
		}
		return &projectionMarker{Mode: MaterializationSymlink, Source: absTarget}, nil
	}
	data, err := os.ReadFile(filepath.Join(path, projectionMarkerName))
	if err != nil {
		return nil, err
	}
	var marker projectionMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, err
	}
	return &marker, nil
}

func writeProjectionMarker(path string, entry *SkillEntry, scope, target string) error {
	marker := projectionMarker{
		Name:   entry.Name,
		Scope:  scope,
		Target: target,
		Mode:   entry.MaterializationMode,
		Source: entry.InstalledPath,
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return writeWithBackup(filepath.Join(path, projectionMarkerName), data)
}

func managedProjectionState(path string) (bool, *projectionMarker) {
	marker, err := readManagedProjection(path)
	if err != nil {
		return false, nil
	}
	return true, marker
}

func targetMatches(target string, targets []string) bool {
	return slices.Contains(NormalizeTargets(targets), NormalizePeer(target))
}
