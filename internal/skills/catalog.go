package skills

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type CatalogEntry struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	DetailURL string `json:"detail_url,omitempty"`
	Source    string `json:"source,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Subdir    string `json:"subdir,omitempty"`
}

var (
	catalogLeaderboardPattern = regexp.MustCompile(`href="(/([^"/]+/[^"/]+/[^"/]+))"[^>]*>\s*(?:\d+\s+)?([a-zA-Z0-9._-]+)\s+([a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+)\s+`)
	catalogInstallPattern     = regexp.MustCompile(`npx\s+skills\s+add\s+([^\s<]+)\s+--skill\s+([a-zA-Z0-9._-]+)`)
	catalogFetchURL           = fetchURL
	catalogListLinePattern    = regexp.MustCompile(`^\s*(?:[-*]\s*)?([a-zA-Z0-9._ -]+?)\s+\[([a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+)\]\s*$`)
	catalogPlainLinePattern   = regexp.MustCompile(`^\s*([a-zA-Z0-9._-]+)\s+([a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+)\s*$`)
	runSkillsFind             = execSkillsFind
)

func SearchCatalog(query string) ([]CatalogEntry, error) {
	stdout, err := runSkillsFind(query)
	if err != nil {
		cached, cacheErr := LoadCatalogCache("skills.sh")
		if cacheErr != nil {
			return nil, err
		}
		return filterCatalogEntries(cached, query), nil
	}
	entries := parseSkillsFindOutput(stdout)
	if len(entries) == 0 {
		cached, cacheErr := LoadCatalogCache("skills.sh")
		if cacheErr == nil {
			return filterCatalogEntries(cached, query), nil
		}
		return nil, fmt.Errorf("no catalog entries found")
	}
	_ = SaveCatalogCache("skills.sh", entries)
	return filterCatalogEntries(entries, query), nil
}

func ResolveCatalogInstall(name string) (CatalogEntry, error) {
	results, err := SearchCatalog(name)
	if err != nil {
		return CatalogEntry{}, err
	}
	name = NormalizeName(name)
	var match *CatalogEntry
	for i := range results {
		if NormalizeName(results[i].Name) == name {
			match = &results[i]
			break
		}
	}
	if match == nil && len(results) > 0 {
		match = &results[0]
	}
	if match == nil {
		return CatalogEntry{}, fmt.Errorf("catalog skill not found: %s", name)
	}

	source, skillName, err := fetchCatalogInstallMeta(*match)
	if err != nil {
		return CatalogEntry{}, err
	}
	match.Source = source
	match.Subdir = skillName
	return *match, nil
}

func InstallFromCatalog(name string, opts ...InstallOptions) (*SkillEntry, error) {
	entry, err := ResolveCatalogInstall(name)
	if err != nil {
		return nil, err
	}
	installOpts := InstallOptions{
		Name:       entry.Name,
		SourceType: SourceTypeGit,
		SourceKind: SourceKindCatalog,
		Source:     entry.Source,
		Ref:        entry.Ref,
		Subdir:     entry.Subdir,
	}
	if len(opts) > 0 {
		override := opts[0]
		installOpts.Name = firstNonEmpty(override.Name, installOpts.Name)
		installOpts.Scope = override.Scope
		installOpts.SourceKind = firstNonEmpty(override.SourceKind, installOpts.SourceKind)
		installOpts.Targets = override.Targets
		installOpts.MaterializationMode = override.MaterializationMode
	}
	return Install(installOpts)
}

func SaveCatalogCache(name string, entries []CatalogEntry) error {
	root, err := CatalogRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, NormalizeName(name)+".json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return writeWithBackup(path, data)
}

func LoadCatalogCache(name string) ([]CatalogEntry, error) {
	root, err := CatalogRoot()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, NormalizeName(name)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []CatalogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func fetchSkillsShCatalog() ([]CatalogEntry, error) {
	baseURL := skillsShBaseURL()
	body, err := catalogFetchURL(baseURL)
	if err != nil {
		return nil, err
	}
	matches := catalogLeaderboardPattern.FindAllStringSubmatch(body, -1)
	entries := make([]CatalogEntry, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 5 {
			continue
		}
		name := NormalizeName(match[3])
		repo := strings.TrimSpace(match[4])
		key := repo + "::" + name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		entries = append(entries, CatalogEntry{
			Name:      name,
			Repo:      repo,
			DetailURL: resolveCatalogURL(baseURL, match[1]),
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no catalog entries found")
	}
	return entries, nil
}

func fetchCatalogInstallMeta(entry CatalogEntry) (string, string, error) {
	if strings.TrimSpace(entry.DetailURL) == "" {
		return "", "", fmt.Errorf("catalog detail URL missing")
	}
	body, err := catalogFetchURL(entry.DetailURL)
	if err != nil {
		return "", "", err
	}
	match := catalogInstallPattern.FindStringSubmatch(body)
	if len(match) < 3 {
		return "", "", fmt.Errorf("install command not found for %s", entry.Name)
	}
	source := strings.TrimSpace(match[1])
	skillName := NormalizeName(match[2])
	return source, skillName, nil
}

func filterCatalogEntries(entries []CatalogEntry, query string) []CatalogEntry {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return entries
	}
	filtered := make([]CatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Name), query) || strings.Contains(strings.ToLower(entry.Repo), query) {
			filtered = append(filtered, entry)
		}
	}
	slices.SortFunc(filtered, func(a, b CatalogEntry) int {
		return strings.Compare(a.Name, b.Name)
	})
	return filtered
}

func parseSkillsFindOutput(stdout string) []CatalogEntry {
	lines := strings.Split(stdout, "\n")
	entries := make([]CatalogEntry, 0, len(lines))
	seen := map[string]struct{}{}
	baseURL := skillsShBaseURL()
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var (
			name string
			repo string
		)
		if match := catalogListLinePattern.FindStringSubmatch(line); len(match) == 3 {
			name = NormalizeName(match[1])
			repo = strings.TrimSpace(match[2])
		} else if match := catalogPlainLinePattern.FindStringSubmatch(line); len(match) == 3 {
			name = NormalizeName(match[1])
			repo = strings.TrimSpace(match[2])
		} else {
			continue
		}
		key := repo + "::" + name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		entries = append(entries, CatalogEntry{
			Name:      name,
			Repo:      repo,
			DetailURL: resolveCatalogURL(baseURL, "/"+repo+"/"+name),
		})
	}
	return entries
}

func skillsShBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("SPARK_SKILLS_SH_BASE_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return "https://skills.sh"
}

func resolveCatalogURL(baseURL, ref string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + ref
	}
	u, err := base.Parse(ref)
	if err != nil {
		return baseURL + ref
	}
	return u.String()
}

func fetchURL(rawURL string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %s: %s", rawURL, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func execSkillsFind(query string) (string, error) {
	cmd := exec.Command("npx", "skills", "find", query)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("npx skills find failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
