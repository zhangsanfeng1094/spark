package integrations

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

func decodeResponsesRequest(r *http.Request) (map[string]any, string, error) {
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
	raw := truncateForLog(string(data), 16*1024)

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

func truncateForLog(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func mustJSONForLog(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return truncateForLog(string(data), 16*1024)
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
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return nil, "", err
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
	path := filepath.Join(w.dir, fmt.Sprintf("%s-%s%s", w.baseName, day, w.ext))
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
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -(w.keepDays - 1))
	prefix := w.baseName + "-"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, w.ext) {
			continue
		}
		dayPart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), w.ext)
		fileDay, err := time.Parse("2006-01-02", dayPart)
		if err != nil {
			continue
		}
		if fileDay.Before(cutoff) {
			_ = os.Remove(filepath.Join(w.dir, name))
		}
	}
}

func openProxyLogFile(envKey, defaultFileName, prefix string) (io.WriteCloser, string, error) {
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
	_, _ = fmt.Fprintf(w, "%s [%s] logger initialized\n", time.Now().Format(time.RFC3339), prefix)
	return w, rollingPath, nil
}

func newStreamingHTTPClient() *http.Client {
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
