package proxyutil

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

func DecodeResponsesRequest(r *http.Request) (map[string]any, string, error) {
	var reader io.Reader = r.Body
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, "", fmt.Errorf("invalid gzip body")
		}
		defer zr.Close()
		reader = zr
	case "zstd":
		zr, err := zstd.NewReader(r.Body)
		if err != nil {
			return nil, "", fmt.Errorf("invalid zstd body")
		}
		defer zr.Close()
		reader = zr
	default:
		return nil, "", fmt.Errorf("unsupported content-encoding: %s", r.Header.Get("Content-Encoding"))
	}

	data, err := io.ReadAll(io.LimitReader(reader, 8*1024*1024))
	if err != nil {
		return nil, "", fmt.Errorf("read body failed")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty body")
	}
	raw := TruncateForLog(string(data), 16*1024)

	var req map[string]any
	if err := json.Unmarshal(data, &req); err == nil {
		return req, raw, nil
	}

	var quoted string
	if err := json.Unmarshal(data, &quoted); err == nil {
		var nested map[string]any
		if err := json.Unmarshal([]byte(quoted), &nested); err == nil {
			return nested, raw, nil
		}
	}
	return nil, raw, fmt.Errorf("malformed object")
}

func TruncateForLog(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func MustJSONForLog(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return TruncateForLog(string(data), 16*1024)
}

type dailyRollingLogWriter struct {
	mu          sync.Mutex
	dir         string
	baseName    string
	ext         string
	keepDays    int
	currentDay  string
	current     *os.File
	currentPath string
}

var (
	launchRouteLogMu     sync.Mutex
	launchRouteLogWriter io.WriteCloser
	launchRouteLogPath   string
)

func newDailyRollingLogWriter(basePath string, keepDays int) (*dailyRollingLogWriter, string, error) {
	if keepDays <= 0 {
		keepDays = 7
	}
	w := &dailyRollingLogWriter{
		dir:      filepath.Dir(basePath),
		baseName: strings.TrimSuffix(filepath.Base(basePath), filepath.Ext(basePath)),
		ext:      filepath.Ext(basePath),
		keepDays: keepDays,
	}
	if err := w.rotateLocked(time.Now()); err != nil {
		return nil, "", err
	}
	return w, w.currentPath, nil
}

func (w *dailyRollingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateLocked(time.Now()); err != nil {
		return 0, err
	}
	if w.current == nil {
		return 0, fmt.Errorf("log file is closed")
	}
	return w.current.Write(p)
}

func (w *dailyRollingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil {
		return nil
	}
	err := w.current.Close()
	w.current = nil
	return err
}

func (w *dailyRollingLogWriter) rotateLocked(now time.Time) error {
	day := now.Format("2006-01-02")
	if day == w.currentDay && w.current != nil {
		return nil
	}
	if w.current != nil {
		_ = w.current.Close()
		w.current = nil
	}
	dayDir := filepath.Join(w.dir, now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dayDir, w.baseName+w.ext)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.current = f
	w.currentDay = day
	w.currentPath = path
	w.cleanupLocked(now)
	return nil
}

func (w *dailyRollingLogWriter) cleanupLocked(now time.Time) {
	cutoff := now.AddDate(0, 0, -(w.keepDays - 1))
	_ = filepath.WalkDir(w.dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != w.baseName+w.ext {
			return nil
		}
		rel, err := filepath.Rel(w.dir, filepath.Dir(path))
		if err != nil {
			return nil
		}
		fileDay, err := time.Parse("2006/01/02", filepath.ToSlash(rel))
		if err != nil {
			return nil
		}
		if fileDay.Before(cutoff) {
			_ = os.Remove(path)
		}
		return nil
	})
}

func OpenProxyLogFile(envKey, defaultFileName, prefix string) (io.WriteCloser, string, error) {
	return OpenProxyLogFileWithInit(envKey, defaultFileName, prefix, true)
}

func OpenProxyLogFileWithInit(envKey, defaultFileName, prefix string, writeInitLine bool) (io.WriteCloser, string, error) {
	logPath := strings.TrimSpace(os.Getenv(envKey))
	if logPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", err
		}
		logPath = filepath.Join(home, ".spark", "logs", defaultFileName)
	}
	w, rollingPath, err := newDailyRollingLogWriter(logPath, 7)
	if err != nil {
		return nil, "", err
	}
	if writeInitLine {
		_, _ = fmt.Fprintf(w, "%s [%s] logger initialized\n", time.Now().Format(time.RFC3339), prefix)
	}
	return w, rollingPath, nil
}

func OpenProxySessionLogFile(currentLogPath, sessionID, prefix string) (io.WriteCloser, string, error) {
	basePath := sessionLogBasePath(currentLogPath, sessionID)
	w, rollingPath, err := newDailyRollingLogWriter(basePath, 7)
	if err != nil {
		return nil, "", err
	}
	_, _ = fmt.Fprintf(w, "%s [%s] session logger initialized session=%s\n", time.Now().Format(time.RFC3339), prefix, SanitizeLogFilePart(sessionID))
	return w, rollingPath, nil
}

func sessionLogBasePath(currentLogPath, sessionID string) string {
	ext := filepath.Ext(currentLogPath)
	dir := filepath.Dir(currentLogPath)
	base := strings.TrimSuffix(filepath.Base(currentLogPath), ext)
	if dayDir, ok := splitDailyLogDir(dir); ok {
		dir = dayDir
	}
	return filepath.Join(dir, base+"-"+SanitizeLogFilePart(sessionID)+ext)
}

func splitDailyLogDir(dir string) (string, bool) {
	day := filepath.Base(dir)
	monthDir := filepath.Dir(dir)
	month := filepath.Base(monthDir)
	yearDir := filepath.Dir(monthDir)
	year := filepath.Base(yearDir)
	if _, err := time.Parse("2006/01/02", strings.Join([]string{year, month, day}, "/")); err != nil {
		return dir, false
	}
	return filepath.Dir(yearDir), true
}

func SanitizeLogFilePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 96 {
			break
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "unknown"
	}
	return out
}

func launchRouteLogger() (io.WriteCloser, string, error) {
	launchRouteLogMu.Lock()
	defer launchRouteLogMu.Unlock()
	if launchRouteLogWriter != nil {
		return launchRouteLogWriter, launchRouteLogPath, nil
	}
	w, path, err := OpenProxyLogFileWithInit("AGENT_LAUNCH_ROUTE_LOG", "launch-route.log", "route", false)
	if err != nil {
		return nil, "", err
	}
	launchRouteLogWriter = w
	launchRouteLogPath = path
	return launchRouteLogWriter, launchRouteLogPath, nil
}

func AppendLaunchRouteLog(line string) string {
	w, path, err := launchRouteLogger()
	if err != nil {
		return ""
	}
	_, _ = fmt.Fprintf(w, "%s\n", strings.TrimSpace(line))
	return path
}

func NewStreamingHTTPClient() *http.Client {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: 0}
	}
	cloned := tr.Clone()
	// SSE streams are safer without transparent gzip decoding, which can surface
	// truncated-compression errors as unexpected EOF before any chunk is parsed.
	cloned.DisableCompression = true
	return &http.Client{
		Timeout:   0,
		Transport: cloned,
	}
}
