package tui

import (
	"testing"

	"spark/internal/config"
)

func TestDetectProviderTypeGemini(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "google generative language", base: "https://generativelanguage.googleapis.com/v1beta", want: "Gemini"},
		{name: "google ai studio alias", base: "https://ai.google.dev/api", want: "Gemini"},
		{name: "openai", base: "https://api.openai.com/v1", want: "OpenAI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectProviderType(&config.Profile{OpenAIBaseURL: tt.base})
			if got != tt.want {
				t.Fatalf("detectProviderType(%q)=%q want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestCreateGeminiProfileFromProviderModal(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.openAddModal()
	for i, opt := range m.providerOptions {
		if opt.kind == "gemini" {
			m.modalCursor = i
			break
		}
	}
	m.createProfileFromModal()

	p := m.cfg.Profiles["gemini"]
	if p == nil {
		t.Fatalf("expected gemini profile, got %#v", m.cfg.Profiles)
	}
	if p.OpenAIBaseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Fatalf("base url mismatch: %#v", p)
	}
	if p.OpenAIAPIType != config.OpenAIAPITypeGeminiGenerateContent {
		t.Fatalf("api type mismatch: %#v", p)
	}
	if len(p.Models) != 1 || p.Models[0] != "gemini-2.5-flash" || p.DefaultModel != "gemini-2.5-flash" {
		t.Fatalf("model defaults mismatch: %#v", p)
	}
	if got := m.fields[pmFieldProviderType].value; got != "Gemini" {
		t.Fatalf("provider field mismatch: %q", got)
	}
}

func TestSelectProviderTypeFieldUpdatesCurrentProfileDraft(t *testing.T) {
	m := newPMModel(&config.RootConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {OpenAIBaseURL: "https://api.openai.com/v1"},
		},
	})
	m.focusArea = pmFocusFields
	m.focusField = pmFieldProviderType

	if !m.openFieldModalIfNeeded() {
		t.Fatal("expected provider type field to open a modal")
	}
	for i, opt := range m.providerOptions {
		if opt.kind == "gemini" {
			m.modalCursor = i
			break
		}
	}
	m.confirmProviderTypeSelection()

	if got := m.fields[pmFieldProviderType].value; got != "Gemini" {
		t.Fatalf("provider field mismatch: %q", got)
	}
	if got := m.fields[pmFieldOpenAIBaseURL].value; got != "https://generativelanguage.googleapis.com/v1beta" {
		t.Fatalf("base url field mismatch: %q", got)
	}
	if got := m.fields[pmFieldOpenAIAPIType].value; got != config.OpenAIAPITypeGeminiGenerateContent {
		t.Fatalf("api type field mismatch: %q", got)
	}
	if len(m.modelsDraft) != 1 || m.modelsDraft[0] != "gemini-2.5-flash" || m.defaultModel != "gemini-2.5-flash" {
		t.Fatalf("model draft mismatch: models=%#v default=%q", m.modelsDraft, m.defaultModel)
	}
	if !m.dirty {
		t.Fatal("expected provider type selection to mark profile dirty")
	}
}
