package integrations

import (
	"reflect"
	"testing"
)

func TestCodexArgs(t *testing.T) {
	c := &Codex{}
	got := c.args("glm-5:cloud", "https://api.example.com/v1", []string{"--no-alt-screen"})
	want := []string{
		"-c", `model_providers.spark.name="Spark"`,
		"-c", `model_providers.spark.base_url="https://api.example.com/v1"`,
		"-c", `model_provider="spark"`,
		"-m", "glm-5:cloud",
		"--no-alt-screen",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codex args mismatch, got %v want %v", got, want)
	}
}
