package app

import "testing"

func TestInteractiveMenuOptionsIncludeLaunchAndManagementActions(t *testing.T) {
	options := interactiveMenuOptions()
	foundQuickLaunch := false
	foundLaunchOptions := false
	foundSettings := false
	foundSkills := false
	foundTokenUsage := false
	for _, option := range options {
		if option == "Quick launch" {
			foundQuickLaunch = true
		}
		if option == "Launch options" {
			foundLaunchOptions = true
		}
		if option == "Manage settings" {
			foundSettings = true
		}
		if option == "Manage skills" {
			foundSkills = true
		}
		if option == "Token usage" {
			foundTokenUsage = true
		}
	}
	if !foundQuickLaunch {
		t.Fatalf("expected Quick launch option, got %v", options)
	}
	if !foundLaunchOptions {
		t.Fatalf("expected Launch options option, got %v", options)
	}
	if !foundSettings {
		t.Fatalf("expected Manage settings option, got %v", options)
	}
	if !foundSkills {
		t.Fatalf("expected Manage skills option, got %v", options)
	}
	if !foundTokenUsage {
		t.Fatalf("expected Token usage option, got %v", options)
	}
}
