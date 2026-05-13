package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"spark/internal/skills"
	"spark/internal/tui"
)

var (
	searchCatalog      = skills.SearchCatalog
	installFromCatalog = skills.InstallFromCatalog
	selectSkillOption  = tui.SelectOne
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			return manageSkills()
		},
	}

	cmd.AddCommand(newSkillListCmd())
	cmd.AddCommand(newSkillShowCmd())
	cmd.AddCommand(newSkillSearchCmd())
	cmd.AddCommand(newSkillInstallCmd())
	cmd.AddCommand(newSkillRemoveCmd())
	cmd.AddCommand(newSkillEnableCmd())
	cmd.AddCommand(newSkillDisableCmd())
	cmd.AddCommand(newSkillSyncCmd())
	cmd.AddCommand(newSkillImportCmd())
	cmd.AddCommand(newSkillUpgradeCmd())
	return cmd
}

func newSkillListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := skills.LoadRegistry()
			if err != nil {
				return err
			}
			if len(registry.Skills) == 0 {
				_, err := io.WriteString(cmd.OutOrStdout(), "No skills configured.\n")
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), describeSkills(registry)+"\n")
			return err
		},
	}
}

func newSkillShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show skill details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := skills.LoadRegistry()
			if err != nil {
				return err
			}
			entry := registry.Skills[skills.NormalizeName(args[0])]
			if entry == nil {
				return fmt.Errorf("skill not found: %s", args[0])
			}
			lines := []string{
				fmt.Sprintf("name: %s", entry.Name),
				fmt.Sprintf("enabled: %t", entry.Enabled),
				fmt.Sprintf("managed: %t", entry.Managed),
				fmt.Sprintf("source_type: %s", entry.SourceType),
				fmt.Sprintf("source: %s", entry.Source),
			}
			if entry.Ref != "" {
				lines = append(lines, fmt.Sprintf("ref: %s", entry.Ref))
			}
			if entry.Subdir != "" {
				lines = append(lines, fmt.Sprintf("subdir: %s", entry.Subdir))
			}
			if entry.InstalledPath != "" {
				lines = append(lines, fmt.Sprintf("installed_path: %s", entry.InstalledPath))
			}
			if len(entry.Targets) > 0 {
				lines = append(lines, fmt.Sprintf("targets: %s", strings.Join(entry.Targets, ",")))
			}
			if entry.Manifest.Description != "" {
				lines = append(lines, fmt.Sprintf("description: %s", entry.Manifest.Description))
			}
			_, err = io.WriteString(cmd.OutOrStdout(), strings.Join(lines, "\n")+"\n")
			return err
		},
	}
}

func newSkillInstallCmd() *cobra.Command {
	var source string
	var sourceType string
	var ref string
	var subdir string
	var targetsCSV string
	cmd := &cobra.Command{
		Use:   "install <name>",
		Short: "Install a skill from a local directory, git source, or catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				entry *skills.SkillEntry
				err   error
			)
			if strings.TrimSpace(source) == "" {
				entry, err = installFromCatalog(args[0])
			} else {
				entry, err = skills.Install(skills.InstallOptions{
					Name:       args[0],
					SourceType: sourceType,
					Source:     source,
					Ref:        ref,
					Subdir:     subdir,
					Targets:    parseCSV(targetsCSV),
				})
			}
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Installed skill %s.\n", entry.Name))
			return err
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "Skill source directory or git repository")
	cmd.Flags().StringVar(&sourceType, "source-type", skills.SourceTypeLocal, "Skill source type: local or git")
	cmd.Flags().StringVar(&ref, "ref", "", "Pinned git ref for git sources")
	cmd.Flags().StringVar(&subdir, "subdir", "", "Subdirectory inside the source")
	cmd.Flags().StringVar(&targetsCSV, "targets", "codex,claude", "Comma-separated peer targets")
	return cmd
}

func newSkillSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search remote skills catalogs and choose a skill to install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := searchCatalog(args[0])
			if err != nil {
				return err
			}
			if len(results) == 0 {
				_, err := io.WriteString(cmd.OutOrStdout(), "No catalog results.\n")
				return err
			}
			options := make([]string, 0, len(results))
			byOption := make(map[string]skills.CatalogEntry, len(results))
			for _, entry := range results {
				label := fmt.Sprintf("%s [%s]", entry.Name, entry.Repo)
				options = append(options, label)
				byOption[label] = entry
			}
			selected, err := selectSkillOption("Select skill to install:", options)
			if err != nil {
				return err
			}
			chosen, ok := byOption[selected]
			if !ok {
				return fmt.Errorf("selected skill not found: %s", selected)
			}
			entry, err := installFromCatalog(chosen.Name)
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Installed skill %s.\n", entry.Name))
			return err
		},
	}
}

func newSkillRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := skills.Remove(args[0]); err != nil {
				return err
			}
			_, err := io.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Removed skill %s.\n", skills.NormalizeName(args[0])))
			return err
		},
	}
}

func newSkillEnableCmd() *cobra.Command {
	return newSkillToggleCmd("enable", true)
}

func newSkillDisableCmd() *cobra.Command {
	return newSkillToggleCmd("disable", false)
}

func newSkillToggleCmd(use string, enabled bool) *cobra.Command {
	label := "Enable"
	if !enabled {
		label = "Disable"
	}
	return &cobra.Command{
		Use:   use + " <name>",
		Short: label + " a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := skills.SetEnabled(args[0], enabled); err != nil {
				return err
			}
			_, err := io.WriteString(cmd.OutOrStdout(), fmt.Sprintf("%sd skill %s.\n", label, skills.NormalizeName(args[0])))
			return err
		},
	}
}

func newSkillSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync <target>",
		Short: "Sync enabled skills to Codex or Claude",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := skills.SyncToPeer(args[0], ""); err != nil {
				return err
			}
			_, err := io.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Synced skills to %s.\n", strings.ToLower(args[0])))
			return err
		},
	}
}

func newSkillImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <source>",
		Short: "Import skills from Codex or Claude",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := skills.ImportFromPeer(args[0], "")
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Imported %d skill(s), skipped %d.\n", result.Added, result.Skipped))
			return err
		},
	}
}

func newSkillUpgradeCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "upgrade <name>",
		Short: "Reinstall a managed skill at a new ref",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := skills.LoadRegistry()
			if err != nil {
				return err
			}
			entry := registry.Skills[skills.NormalizeName(args[0])]
			if entry == nil {
				return fmt.Errorf("skill not found: %s", args[0])
			}
			if !entry.Managed {
				return fmt.Errorf("skill is unmanaged: %s", args[0])
			}
			if _, err := skills.Install(skills.InstallOptions{
				Name:       entry.Name,
				SourceType: entry.SourceType,
				Source:     entry.Source,
				Ref:        ref,
				Subdir:     entry.Subdir,
				Targets:    entry.Targets,
			}); err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Upgraded skill %s.\n", entry.Name))
			return err
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Pinned git ref for the upgraded version")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}

func describeSkills(registry *skills.Registry) string {
	names := make([]string, 0, len(registry.Skills))
	for name := range registry.Skills {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := []string{"Skills:"}
	for _, name := range names {
		entry := registry.Skills[name]
		state := "disabled"
		if entry.Enabled {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", entry.Name, state, entry.SourceType))
	}
	return strings.Join(lines, "\n")
}

func parseCSV(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
