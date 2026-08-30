#!/usr/bin/env node

const path = require("path");
const cp = require("child_process");
const fs = require("fs");

// Platform mapping
const platformMap = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows"
};

const archMap = {
  x64: "x64",
  arm64: "arm64"
};

const platform = platformMap[process.platform];
const arch = archMap[process.arch];
const packageRoot = path.resolve(__dirname, "..");

if (!platform || !arch) {
  console.error(
    `Unsupported platform/arch: ${process.platform}/${process.arch}`
  );
  console.error(
    "Please check https://github.com/zhangsanfeng1094/spark for supported platforms."
  );
  process.exit(1);
}

// Determine package alias (matches optionalDependencies in main package)
// The alias maps to: npm:@ngominhbinh708/spark@VERSION-PLATFORM-ARCH
const packageAlias = `@ngominhbinh708/spark-${platform}-${arch}`;
const binaryName = process.platform === "win32" ? "spark.exe" : "spark";

/**
 * Detect the package manager that owns this global install so `spark update`
 * can re-run the correct command (mirrors Codex's JS shim behavior).
 */
function detectPackageManager() {
  const userAgent = process.env.npm_config_user_agent || "";
  if (/\bbun\//.test(userAgent)) {
    return "bun";
  }
  if (/\bpnpm\//.test(userAgent)) {
    return "pnpm";
  }

  const execPath = process.env.npm_execpath || "";
  if (execPath.includes("bun")) {
    return "bun";
  }
  if (execPath.includes("pnpm")) {
    return "pnpm";
  }

  const root = packageRoot.replace(/\\/g, "/");
  if (root.includes(".bun/install/global") || packageRoot.includes(".bun\\install\\global")) {
    return "bun";
  }
  if (root.includes("/pnpm/") || packageRoot.toLowerCase().includes("\\pnpm\\")) {
    return "pnpm";
  }

  // Walk ancestors for pnpm's .modules.yaml marker.
  let current = packageRoot;
  const filesystemRoot = path.parse(current).root;
  while (true) {
    const nodeModules = path.join(current, "node_modules");
    if (fs.existsSync(path.join(nodeModules, ".modules.yaml"))) {
      return "pnpm";
    }
    if (current === filesystemRoot) {
      break;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      break;
    }
    current = parent;
  }

  return "npm";
}

let binaryPath;
try {
  // Try to resolve the platform-specific package via alias
  // This resolves the npm alias: @ngominhbinh708/spark-linux-x64 -> @ngominhbinh708/spark@VERSION-linux-x64
  const packageJsonPath = require.resolve(`${packageAlias}/package.json`);
  const vendorDir = path.join(path.dirname(packageJsonPath), "vendor");
  binaryPath = path.join(vendorDir, binaryName);
} catch (e) {
  // Package not installed
  const packageManager = detectPackageManager();
  const updateCommand =
    packageManager === "bun"
      ? "bun install -g @ngominhbinh708/spark@latest"
      : packageManager === "pnpm"
        ? "pnpm add -g @ngominhbinh708/spark@latest"
        : "npm install -g @ngominhbinh708/spark@latest";
  console.error(`Platform package not found: ${packageAlias}`);
  console.error("");
  console.error("This may happen if:");
  console.error("  1. npm optionalDependencies was skipped due to a platform mismatch");
  console.error("  2. The package was not installed correctly");
  console.error("");
  console.error("Try reinstalling:");
  console.error(`  ${updateCommand}`);
  console.error("  # or: spark update");
  console.error("");
  console.error("Or check if your platform is supported:");
  console.error("  Platform: " + process.platform);
  console.error("  Arch: " + process.arch);
  process.exit(1);
}

// Check if binary exists
if (!fs.existsSync(binaryPath)) {
  console.error(`Binary not found: ${binaryPath}`);
  console.error("The platform package was installed but the binary is missing.");
  console.error("Try reinstalling:");
  console.error("  npm install -g @ngominhbinh708/spark@latest");
  console.error("  # or: spark update");
  process.exit(1);
}

const packageManager = detectPackageManager();
const packageManagerEnvVar =
  packageManager === "bun"
    ? "SPARK_MANAGED_BY_BUN"
    : packageManager === "pnpm"
      ? "SPARK_MANAGED_BY_PNPM"
      : "SPARK_MANAGED_BY_NPM";

const env = {
  ...process.env,
  SPARK_MANAGED_PACKAGE_ROOT: packageRoot,
};
delete env.SPARK_MANAGED_BY_NPM;
delete env.SPARK_MANAGED_BY_BUN;
delete env.SPARK_MANAGED_BY_PNPM;
env[packageManagerEnvVar] = "1";

// Execute the binary with all arguments
const result = cp.spawnSync(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

process.exit(result.status === null ? 1 : result.status);
