package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ReplaceExecutable downloads the release binary for this platform and atomically
// replaces targetPath (usually the current spark executable).
func ReplaceExecutable(ctx context.Context, targetPath string, release *Release) error {
	if release == nil {
		return fmt.Errorf("release is required")
	}
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return fmt.Errorf("target path is required")
	}

	assetName, err := PlatformBinaryName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	asset, ok := release.AssetByName(assetName)
	if !ok || strings.TrimSpace(asset.BrowserDownloadURL) == "" {
		// Fall back to constructed download URL when the API omitted assets.
		asset = ReleaseAsset{
			Name:               assetName,
			BrowserDownloadURL: DownloadURL(DefaultGitHubRepo, release.Version(), assetName),
		}
	}

	dir := filepath.Dir(targetPath)
	tmpFile, err := os.CreateTemp(dir, ".spark-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := downloadTo(ctx, asset.BrowserDownloadURL, tmpFile); err != nil {
		return err
	}
	if err := tmpFile.Chmod(0o755); err != nil {
		// Best-effort on platforms that support chmod.
		_ = err
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Optional checksum verification when checksums.txt is present.
	if sumAsset, ok := release.AssetByName("checksums.txt"); ok && sumAsset.BrowserDownloadURL != "" {
		if err := verifySHA256(ctx, tmpPath, assetName, sumAsset.BrowserDownloadURL); err != nil {
			return err
		}
	}

	backupPath := targetPath + ".bak"
	_ = os.Remove(backupPath)

	if err := os.Rename(targetPath, backupPath); err != nil {
		// On Windows the running binary may be locked; try direct overwrite via copy.
		if copyErr := copyFileReplace(tmpPath, targetPath); copyErr != nil {
			return fmt.Errorf("replace executable: rename failed (%v); copy failed (%w)", err, copyErr)
		}
		_ = os.Remove(tmpPath)
		return nil
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		// Roll back.
		_ = os.Rename(backupPath, targetPath)
		return fmt.Errorf("install new binary: %w", err)
	}
	_ = os.Remove(backupPath)
	// Prevent defer from removing the installed file.
	tmpPath = ""
	return nil
}

func downloadTo(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "spark-agent-launcher")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("download %s: status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	return nil
}

func verifySHA256(ctx context.Context, filePath, assetName, checksumsURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "spark-agent-launcher")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		// Checksums are best-effort; network failures should not block updates.
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	expected, ok := parseChecksum(string(data), assetName)
	if !ok {
		return nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch for %s: got %s want %s", assetName, got, expected)
	}
	return nil
}

// parseChecksum reads GoReleaser-style "hex  name" lines.
func parseChecksum(content, assetName string) (string, bool) {
	assetName = strings.TrimSpace(assetName)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := fields[0]
		name := fields[len(fields)-1]
		name = strings.TrimPrefix(name, "./")
		name = filepath.Base(name)
		if name == assetName && len(sum) >= 32 {
			return sum, true
		}
	}
	return "", false
}

func copyFileReplace(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
