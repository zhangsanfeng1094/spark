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
	want = []string{"profile-default-model", "profile-model-a", "profile-model-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default model precedence mismatch, got %v want %v", got, want)
	}

	got = resolveModels("", &config.Profile{DefaultModel: "profile-model"})
	want = []string{"profile-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default model fallback mismatch, got %v want %v", got, want)
	}
}

func TestResolveModelsDefaultModelDedupAndReorder(t *testing.T) {
	profile := &config.Profile{
		Models:       []string{"model-a", "model-b", "model-a"},
		DefaultModel: "model-b",
	}

	got := resolveModels("", profile)
	want := []string{"model-b", "model-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default model reorder mismatch, got %v want %v", got, want)
	}
}

func TestResolveModelsStripsNUL(t *testing.T) {
	profile := &config.Profile{
		Models:       []string{"glm-5\x00", "other"},
		DefaultModel: " glm-5\x00 ",
	}

	got := resolveModels("", profile)
	want := []string{"glm-5", "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveModels mismatch, got %v want %v", got, want)
	}

	got = resolveModels(" glm-5\x00 ", profile)
	want = []string{"glm-5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveModels flag mismatch, got %v want %v", got, want)
	}
}
