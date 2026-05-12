package skills

import "time"

const CurrentRegistryVersion = 1

const (
	SourceTypeGit   = "git"
	SourceTypeLocal = "local"
)

type Registry struct {
	Version  int                    `json:"version"`
	Skills   map[string]*SkillEntry `json:"skills"`
	Catalogs []CatalogRef           `json:"catalogs,omitempty"`
}

type SkillEntry struct {
	Name          string        `json:"name"`
	SourceType    string        `json:"source_type"`
	Source        string        `json:"source"`
	Ref           string        `json:"ref,omitempty"`
	Subdir        string        `json:"subdir,omitempty"`
	Enabled       bool          `json:"enabled"`
	Targets       []string      `json:"targets,omitempty"`
	Managed       bool          `json:"managed"`
	InstalledPath string        `json:"installed_path,omitempty"`
	Manifest      SkillManifest `json:"manifest,omitempty"`
	InstalledAt   time.Time     `json:"installed_at,omitempty"`
}

type SkillManifest struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
}

type CatalogRef struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type InstallOptions struct {
	Name       string
	SourceType string
	Source     string
	Ref        string
	Subdir     string
	Targets    []string
}

type ImportResult struct {
	Added   int
	Skipped int
}
