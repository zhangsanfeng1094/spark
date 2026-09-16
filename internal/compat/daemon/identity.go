package daemon

import (
	"os"
	"path/filepath"

	"spark/internal/version"
)

// BinaryIdentity identifies the Spark binary that should own the shared daemon.
// Release builds differ by version/commit; `go run` / local rebuilds differ by
// executable path, size, and mtime even when Version stays "dev".
type BinaryIdentity struct {
	Version  string `json:"version"`
	ExePath  string `json:"exe_path,omitempty"`
	ExeMTime int64  `json:"exe_mtime,omitempty"`
	ExeSize  int64  `json:"exe_size,omitempty"`
}

func CurrentIdentity() BinaryIdentity {
	id := BinaryIdentity{Version: version.Get().String()}
	exe, err := os.Executable()
	if err != nil {
		return id
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	id.ExePath = exe
	if st, err := os.Stat(exe); err == nil {
		id.ExeMTime = st.ModTime().UnixNano()
		id.ExeSize = st.Size()
	}
	return id
}

func (id BinaryIdentity) Matches(other BinaryIdentity) bool {
	if id.Version != other.Version {
		return false
	}
	if id.ExePath == "" || other.ExePath == "" || id.ExePath != other.ExePath {
		return false
	}
	if id.ExeMTime == 0 || other.ExeMTime == 0 || id.ExeMTime != other.ExeMTime {
		return false
	}
	if id.ExeSize == 0 || other.ExeSize == 0 || id.ExeSize != other.ExeSize {
		return false
	}
	return true
}

func (hr *HealthResponse) Identity() BinaryIdentity {
	if hr == nil {
		return BinaryIdentity{}
	}
	return BinaryIdentity{
		Version:  hr.Version,
		ExePath:  hr.ExePath,
		ExeMTime: hr.ExeMTime,
		ExeSize:  hr.ExeSize,
	}
}

func (hr *HealthResponse) MatchesCurrent(current BinaryIdentity) bool {
	if hr == nil || hr.Status != "ok" {
		return false
	}
	return current.Matches(hr.Identity())
}
