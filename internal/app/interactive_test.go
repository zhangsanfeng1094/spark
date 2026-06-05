package app

import "testing"

func TestInteractiveMenuOptionsIncludeSkills(t *testing.T) {
	options := interactiveMenuOptions()
	foundSkills := false
	foundTokenUsage := false
	foundLaunchWithProfile := false
	foundPrompts := false
	for _, option := range options {
		if option == "Manage skills" {
			foundSkills = true
		}
		if option == "Manage prompts" {
			foundPrompts = true
		}
		if option == "Token usage" {
			foundTokenUsage = true
		}
		if option == "Launch with profile" {
			foundLaunchWithProfile = true
		}
	}
	if !foundSkills {
		t.Fatalf("expected Manage skills option, got %v", options)
	}
	if !foundTokenUsage {
		t.Fatalf("expected Token usage option, got %v", options)
	}
	if !foundLaunchWithProfile {
		t.Fatalf("expected Launch with profile option, got %v", options)
	}
	if !foundPrompts {
		t.Fatalf("expected Manage prompts option, got %v", options)
	}
}
