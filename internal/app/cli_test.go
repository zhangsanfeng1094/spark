package app

import (
	"reflect"
	"testing"

	"spark/internal/config"
)

func TestProfileNamesSorted(t *testing.T) {
	cfg := &config.RootConfig{
		Profiles: map[string]*config.Profile{
			"zeta":  {},
			"alpha": {},
			"beta":  {},
		},
	}

	got := profileNames(cfg)
	want := []string{"alpha", "beta", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profileNames mismatch, got %v want %v", got, want)
	}
}

func TestResolveModelsPrecedence(t *testing.T) {
	profile := &config.Profile{
		Models:       []string{"profile-model-a", "profile-model-b"},
		DefaultModel: "profile-default-model",
	}

	got := resolveModels("flag-model", profile)
	want := []string{"flag-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flag precedence mismatch, got %v want %v", got, want)
	}

	got = resolveModels("", profile)
	want = []string{"profile-model-a", "profile-model-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profile models precedence mismatch, got %v want %v", got, want)
	}

	got = resolveModels("", &config.Profile{DefaultModel: "profile-model"})
	want = []string{"profile-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default model fallback mismatch, got %v want %v", got, want)
	}
}
