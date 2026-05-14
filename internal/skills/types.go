package skills

import "time"

const CurrentRegistryVersion = 2

const (
	SourceTypeGit   = "git"
	SourceTypeLocal = "local"
)

const (
	SourceKindLocal    = "local"
	SourceKindGit      = "git"
	SourceKindCatalog  = "catalog"
	SourceKindImported = "imported"
)

const (
	ScopeProject = "project"
	ScopeGlobal  = "global"
)

const (
	MaterializationCopy    = "copy"
	MaterializationSymlink = "symlink"
)

type Registry struct {
	Version  int                    `json:"version"`
	Skills   map[string]*SkillEntry `json:"skills"`
	Catalogs []CatalogRef           `json:"catalogs,omitempty"`
}

type SkillEntry struct {
	Name                string        `json:"name"`
	Scope               string        `json:"scope,omitempty"`
	AgentTargets        []string      `json:"agent_targets,omitempty"`
	SourceKind          string        `json:"source_kind,omitempty"`
	MaterializationMode string        `json:"materialization_mode,omitempty"`
	SourceType          string        `json:"source_type,omitempty"`
	Source              string        `json:"source"`
	Ref                 string        `json:"ref,omitempty"`
	Subdir              string        `json:"subdir,omitempty"`
	Enabled             bool          `json:"enabled"`
	Targets             []string      `json:"targets,omitempty"`
	Managed             bool          `json:"managed"`
	InstalledPath       string        `json:"installed_path,omitempty"`
	Manifest            SkillManifest `json:"manifest,omitempty"`
	InstalledAt         time.Time     `json:"installed_at,omitempty"`
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
	Name                string
	Scope               string
	SourceType          string
	SourceKind          string
	Source              string
	Ref                 string
	Subdir              string
	Targets             []string
	MaterializationMode string
}

type SyncOptions struct {
	Scope        string
	Targets      []string
	ProjectRoot  string
	OverrideRoot string
}

type ImportResult struct {
	Added   int
	Skipped int
	Invalid int
}

type ImportOptions struct {
	Scope        string
	Targets      []string
	ProjectRoot  string
	OverrideRoot string
}

type SyncResult struct {
	Added   int
	Updated int
	Skipped int
	Cleaned int
}

type SkillRoot struct {
	Scope  string
	Target string
	Path   string
}

type ProjectionStatus struct {
	Scope  string
	Target string
	Path   string
	State  string
}
