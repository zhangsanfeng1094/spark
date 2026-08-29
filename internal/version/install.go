package version

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Install method environment variables set by the npm/bun/pnpm JS shim.
const (
	EnvManagedByNPM       = "SPARK_MANAGED_BY_NPM"
	EnvManagedByBun       = "SPARK_MANAGED_BY_BUN"
	EnvManagedByPNPM      = "SPARK_MANAGED_BY_PNPM"
	EnvManagedPackageRoot = "SPARK_MANAGED_PACKAGE_ROOT"
)

// InstallMethod describes how the running spark binary was installed.
type InstallMethod string

const (
	InstallNPM        InstallMethod = "npm"
	InstallBun        InstallMethod = "bun"
	InstallPNPM       InstallMethod = "pnpm"
	InstallStandalone InstallMethod = "standalone"
	InstallUnknown    InstallMethod = "unknown"
)

// InstallInfo is the detected install provenance for the current process.
type InstallInfo struct {
	Method      InstallMethod
	PackageRoot string // npm package root when package-managed
	Executable  string // resolved spark binary path
}

// UpdateCommand returns the package-manager command used to install/update spark.
func (i InstallInfo) UpdateCommand() string {
	pkg := NPMPackageName + "@latest"
	switch i.Method {
	case InstallNPM:
		return "npm install -g " + pkg
	case InstallBun:
		return "bun install -g " + pkg
	case InstallPNPM:
		return "pnpm add -g " + pkg
	case InstallStandalone:
		return "spark update"
	default:
		return "npm install -g " + pkg
	}
}

// PackageManaged reports whether updates should go through a Node package manager.
func (i InstallInfo) PackageManaged() bool {
	switch i.Method {
	case InstallNPM, InstallBun, InstallPNPM:
		return true
	default:
		return false
	}
}

// DetectInstallInfo inspects environment variables and the current executable path.
func DetectInstallInfo() (InstallInfo, error) {
	execPath, err := os.Executable()
	if err != nil {
		return InstallInfo{Method: InstallUnknown}, err
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}

	info := InstallInfo{
		Executable:  execPath,
		PackageRoot: strings.TrimSpace(os.Getenv(EnvManagedPackageRoot)),
	}

	switch {
	case envTruthy(os.Getenv(EnvManagedByPNPM)):
		info.Method = InstallPNPM
	case envTruthy(os.Getenv(EnvManagedByBun)):
		info.Method = InstallBun
	case envTruthy(os.Getenv(EnvManagedByNPM)):
		info.Method = InstallNPM
	default:
		info.Method = detectMethodFromPath(execPath, info.PackageRoot)
	}

	if info.PackageRoot == "" && info.PackageManaged() {
		if root := packageRootFromBinaryPath(execPath); root != "" {
			info.PackageRoot = root
		}
	}

	return info, nil
}

func detectMethodFromPath(execPath, packageRoot string) InstallMethod {
	candidates := []string{execPath, packageRoot}
	for _, candidate := range candidates {
		normalized := filepath.ToSlash(strings.ToLower(candidate))
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "/.bun/install/global/") || strings.Contains(normalized, "\\.bun\\install\\global\\") {
			return InstallBun
		}
		if strings.Contains(normalized, "/pnpm/") || strings.Contains(normalized, "\\pnpm\\") {
			// pnpm store or global layouts often include "pnpm" in the path.
			if strings.Contains(normalized, "node_modules") {
				return InstallPNPM
			}
		}
		if strings.Contains(normalized, "node_modules/@ngominhbinh708/spark") ||
			strings.Contains(normalized, "node_modules\\@ngominhbinh708\\spark") {
			return InstallNPM
		}
		if strings.Contains(normalized, "node_modules") {
			return InstallNPM
		}
	}
	if packageRoot != "" {
		return InstallNPM
	}
	return InstallStandalone
}

// packageRootFromBinaryPath walks up from a vendor binary to the main package root.
// Typical layout: <root>/node_modules/@ngominhbinh708/spark-<plat>/vendor/spark
// or nested optional dependency under the main package.
func packageRootFromBinaryPath(execPath string) string {
	dir := filepath.Dir(execPath)
	for i := 0; i < 8; i++ {
		base := filepath.Base(dir)
		parent := filepath.Dir(dir)
		// vendor/spark -> package dir
		if base == "vendor" {
			pkgDir := parent
			pkgBase := filepath.Base(pkgDir)
			if strings.HasPrefix(pkgBase, "spark") {
				// Prefer the main package if this is a nested optional dep.
				grand := filepath.Dir(pkgDir)
				if filepath.Base(grand) == "@ngominhbinh708" {
					main := filepath.Join(filepath.Dir(grand), "@ngominhbinh708", "spark")
					if st, err := os.Stat(filepath.Join(main, "package.json")); err == nil && !st.IsDir() {
						return main
					}
					return pkgDir
				}
				return pkgDir
			}
		}
		if base == "spark" && filepath.Base(parent) == "@ngominhbinh708" {
			if st, err := os.Stat(filepath.Join(dir, "package.json")); err == nil && !st.IsDir() {
				return dir
			}
		}
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// CurrentPlatformLabel returns a short os/arch label for display.
func CurrentPlatformLabel() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}
