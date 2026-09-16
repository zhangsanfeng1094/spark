package daemon

import "testing"

func TestBinaryIdentity_Matches(t *testing.T) {
	t.Parallel()
	current := BinaryIdentity{
		Version:  "spark dev (commit: unknown, built: unknown)",
		ExePath:  "/tmp/spark",
		ExeMTime: 100,
		ExeSize:  50,
	}
	if !current.Matches(current) {
		t.Fatal("identical identity should match")
	}
	other := current
	other.ExeMTime = 101
	if current.Matches(other) {
		t.Fatal("rebuilt binary with new mtime should not match")
	}
	other = current
	other.ExePath = "/tmp/go-build/exe/spark"
	if current.Matches(other) {
		t.Fatal("go run temp binary should not match a previous daemon")
	}
}

func TestHealthResponse_MatchesCurrentLegacyDaemon(t *testing.T) {
	t.Parallel()
	current := BinaryIdentity{
		Version:  "spark dev (commit: unknown, built: unknown)",
		ExePath:  "/tmp/spark",
		ExeMTime: 100,
		ExeSize:  50,
	}
	legacy := &HealthResponse{
		Status:  "ok",
		Version: "spark dev (commit: unknown, built: unknown)",
	}
	if legacy.MatchesCurrent(current) {
		t.Fatal("legacy daemon without binary identity must be restarted")
	}
}
