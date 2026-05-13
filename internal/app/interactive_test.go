package app

import "testing"

func TestInteractiveMenuOptionsIncludeSkills(t *testing.T) {
	options := interactiveMenuOptions()
	found := false
	for _, option := range options {
		if option == "Manage skills" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Manage skills option, got %v", options)
	}
}
