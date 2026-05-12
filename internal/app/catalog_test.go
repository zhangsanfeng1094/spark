package app

import (
	"spark/internal/skills"
)

func stubSkillCatalogForApp(entries []skills.CatalogEntry) func() {
	original := searchCatalog
	searchCatalog = func(query string) ([]skills.CatalogEntry, error) {
		return entries, nil
	}
	return func() {
		searchCatalog = original
	}
}

func stubSkillSelector(fn func(title string, options []string) (string, error)) func() {
	original := selectSkillOption
	selectSkillOption = fn
	return func() {
		selectSkillOption = original
	}
}

func stubSkillInstallFromCatalog(fn func(name string) (*skills.SkillEntry, error)) func() {
	original := installFromCatalog
	installFromCatalog = fn
	return func() {
		installFromCatalog = original
	}
}
