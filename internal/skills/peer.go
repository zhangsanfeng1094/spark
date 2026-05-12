package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func DefaultPeerRoot(peer string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch NormalizePeer(peer) {
	case "codex":
		return filepath.Join(home, ".codex", "skills"), nil
	case "claude":
		return filepath.Join(home, ".claude", "skills"), nil
	default:
		return "", fmt.Errorf("unsupported skill peer: %s", peer)
	}
}

func SyncToPeer(peer, root string) error {
	peer = NormalizePeer(peer)
	if peer == "" {
		return fmt.Errorf("unsupported skill peer: %s", peer)
	}
	if root == "" {
		var err error
		root, err = DefaultPeerRoot(peer)
		if err != nil {
			return err
		}
	}
	registry, err := LoadRegistry()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	entries := make([]*SkillEntry, 0, len(registry.Skills))
	for _, entry := range registry.Skills {
		if entry == nil || !entry.Enabled || !targetsPeer(entry.Targets, peer) || entry.InstalledPath == "" {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, entry := range entries {
		if err := copyDir(entry.InstalledPath, filepath.Join(root, entry.Name)); err != nil {
			return err
		}
	}
	return nil
}

func ImportFromPeer(peer, root string) (ImportResult, error) {
	peer = NormalizePeer(peer)
	if peer == "" {
		return ImportResult{}, fmt.Errorf("unsupported skill peer")
	}
	if root == "" {
		var err error
		root, err = DefaultPeerRoot(peer)
		if err != nil {
			return ImportResult{}, err
		}
	}
	registry, err := LoadRegistry()
	if err != nil {
		return ImportResult{}, err
	}
	storeRoot, err := StorageRoot()
	if err != nil {
		return ImportResult{}, err
	}

	result := ImportResult{}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := NormalizeName(entry.Name())
		if _, exists := registry.Skills[name]; exists {
			result.Skipped++
			continue
		}
		peerPath := filepath.Join(root, entry.Name())
		manifest, err := LoadManifest(peerPath)
		if err != nil {
			continue
		}
		targetDir := filepath.Join(storeRoot, name)
		if err := copyDir(peerPath, targetDir); err != nil {
			return result, err
		}
		registry.Skills[name] = &SkillEntry{
			Name:          name,
			SourceType:    SourceTypeLocal,
			Source:        peerPath,
			Enabled:       true,
			Targets:       NormalizeTargets([]string{peer}),
			Managed:       false,
			InstalledPath: targetDir,
			Manifest:      manifest,
		}
		result.Added++
	}
	if err := SaveRegistry(registry); err != nil {
		return result, err
	}
	return result, nil
}

func targetsPeer(targets []string, peer string) bool {
	for _, target := range NormalizeTargets(targets) {
		if target == peer {
			return true
		}
	}
	return false
}
