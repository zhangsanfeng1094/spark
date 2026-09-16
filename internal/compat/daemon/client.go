package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type DaemonInfo struct {
	PID             int    `json:"pid"`
	Addr            string `json:"addr"`
	BaseURL         string `json:"base_url"`
	StartTime       string `json:"start_time"`
	Version         string `json:"version,omitempty"`
	ExePath         string `json:"exe_path,omitempty"`
	ExeMTime        int64  `json:"exe_mtime,omitempty"`
	ExeSize         int64  `json:"exe_size,omitempty"`
	ManagementToken string `json:"management_token,omitempty"`
}

const (
	ProtocolCodex  = "codex"
	ProtocolClaude = "claude"
	ProtocolGemini = "gemini"
)

type HealthResponse struct {
	Status    string   `json:"status"`
	PID       int      `json:"pid"`
	Addr      string   `json:"addr"`
	Version   string   `json:"version"`
	UptimeSec int      `json:"uptime_sec"`
	Protocols []string `json:"protocols,omitempty"`
	ExePath   string   `json:"exe_path,omitempty"`
	ExeMTime  int64    `json:"exe_mtime,omitempty"`
	ExeSize   int64    `json:"exe_size,omitempty"`
}

func (hr *HealthResponse) Supports(protocol string) bool {
	if hr == nil || strings.TrimSpace(protocol) == "" {
		return false
	}
	if len(hr.Protocols) == 0 {
		return protocol == ProtocolCodex || protocol == ProtocolClaude
	}
	for _, p := range hr.Protocols {
		if p == protocol {
			return true
		}
	}
	return false
}

func DaemonInfoPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.json"), nil
}

func writeDaemonInfo(info DaemonInfo) error {
	path, err := DaemonInfoPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "daemon.json.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return nil
}

func readDaemonInfo() (*DaemonInfo, error) {
	path, err := DaemonInfoPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info DaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func removeDaemonInfo() error {
	path, err := DaemonInfoPath()
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}

func ManagementRequest(method, baseURL, path string, token string, body io.Reader) (*http.Response, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://" + DefaultDaemonAddr
	}
	req, err := http.NewRequest(method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set(managementTokenHeader, token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	return client.Do(req)
}

func CheckHealth(baseURL string) (*HealthResponse, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://" + DefaultDaemonAddr
	}
	url := strings.TrimRight(baseURL, "/") + "/health"
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var hr HealthResponse
	if err := json.Unmarshal(body, &hr); err != nil {
		return nil, err
	}
	return &hr, nil
}

// EnsureDaemon checks if the shared daemon is running, and if not, automatically launches it in the background.
// A running daemon that does not match the current Spark binary (dev rebuild, go run, or release upgrade) is stopped and replaced.
func EnsureDaemon(ctx context.Context, logf func(string, ...any)) (*DaemonInfo, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	current := CurrentIdentity()
	defaultBaseURL := "http://" + DefaultDaemonAddr
	if info, hr := lookupRunningDaemon(defaultBaseURL); info != nil && hr != nil {
		if hr.MatchesCurrent(current) {
			logf("reusing existing spark shared daemon at %s (pid: %d)", info.BaseURL, info.PID)
			return info, nil
		}
		logf("restarting spark shared daemon at %s (pid: %d): binary changed (have %s, want %s)", info.BaseURL, info.PID, hr.Identity().ExePath, current.ExePath)
		_ = StopDaemon()
		waitUntilStopped(info.BaseURL, 3*time.Second)
		if info.BaseURL != defaultBaseURL {
			waitUntilStopped(defaultBaseURL, 500*time.Millisecond)
		}
	}

	return startBackgroundDaemon(logf, current)
}

func lookupRunningDaemon(defaultBaseURL string) (*DaemonInfo, *HealthResponse) {
	if info, err := readDaemonInfo(); err == nil && info != nil && info.BaseURL != "" {
		if hr, err := CheckHealth(info.BaseURL); err == nil && hr != nil && hr.Status == "ok" {
			return infoFromHealth(info, hr), hr
		}
	}
	if hr, err := CheckHealth(defaultBaseURL); err == nil && hr != nil && hr.Status == "ok" {
		info := infoFromHealth(&DaemonInfo{BaseURL: defaultBaseURL}, hr)
		_ = writeDaemonInfo(*info)
		return info, hr
	}
	return nil, nil
}

func infoFromHealth(info *DaemonInfo, hr *HealthResponse) *DaemonInfo {
	if info == nil {
		info = &DaemonInfo{}
	}
	out := *info
	if hr != nil {
		if hr.PID > 0 {
			out.PID = hr.PID
		}
		if hr.Addr != "" {
			out.Addr = hr.Addr
		}
		out.Version = hr.Version
		out.ExePath = hr.ExePath
		out.ExeMTime = hr.ExeMTime
		out.ExeSize = hr.ExeSize
	}
	if out.BaseURL == "" && out.Addr != "" {
		out.BaseURL = "http://" + out.Addr
	}
	return &out
}

func startBackgroundDaemon(logf func(string, ...any), current BinaryIdentity) (*DaemonInfo, error) {
	exePath := current.ExePath
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("failed to get executable path: %w", err)
		}
	}

	logf("launching spark shared daemon in background...")
	cmd := exec.Command(exePath, "daemon", "run", "--addr", DefaultDaemonAddr)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start background daemon: %w", err)
	}

	defaultBaseURL := "http://" + DefaultDaemonAddr
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if info, hr := lookupRunningDaemon(defaultBaseURL); info != nil && hr != nil {
			if hr.MatchesCurrent(current) {
				logf("spark shared daemon is ready at %s (pid: %d)", info.BaseURL, info.PID)
				return info, nil
			}
		}
	}

	return nil, errors.New("spark shared daemon failed to start within timeout")
}

func waitUntilStopped(baseURL string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := CheckHealth(baseURL); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// StopDaemon stops the currently running daemon if active.
func StopDaemon() error {
	info, err := readDaemonInfo()
	if err != nil {
		// Try default port
		if hr, err := CheckHealth("http://" + DefaultDaemonAddr); err == nil && hr != nil && hr.PID > 0 {
			info = &DaemonInfo{PID: hr.PID, BaseURL: "http://" + DefaultDaemonAddr}
		} else {
			return errors.New("spark shared daemon is not running")
		}
	}

	if info == nil || info.PID <= 0 {
		return errors.New("no valid daemon PID found")
	}

	process, err := os.FindProcess(info.PID)
	if err != nil {
		_ = removeDaemonInfo()
		return fmt.Errorf("find process error: %w", err)
	}

	baseURL := info.BaseURL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://" + DefaultDaemonAddr
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = removeDaemonInfo()
		return fmt.Errorf("failed to send SIGTERM to daemon: %w", err)
	}
	waitUntilStopped(baseURL, 2*time.Second)
	if _, err := CheckHealth(baseURL); err == nil {
		_ = process.Signal(syscall.SIGKILL)
		waitUntilStopped(baseURL, 1*time.Second)
	}

	_ = removeDaemonInfo()
	return nil
}

// StatusDaemon returns whether the daemon is running and its metadata.
func StatusDaemon() (*DaemonInfo, bool, error) {
	info, err := readDaemonInfo()
	targetURL := "http://" + DefaultDaemonAddr
	if err == nil && info != nil && info.BaseURL != "" {
		targetURL = info.BaseURL
	}

	hr, err := CheckHealth(targetURL)
	if err != nil {
		return nil, false, nil
	}

	if info == nil {
		info = &DaemonInfo{
			PID:     hr.PID,
			Addr:    hr.Addr,
			BaseURL: targetURL,
		}
	} else {
		info.PID = hr.PID
		info.Addr = hr.Addr
	}
	return info, true, nil
}
