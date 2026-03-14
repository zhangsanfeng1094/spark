#!/usr/bin/env node

const path = require("path");
const cp = require("child_process");

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

let binaryPath;
try {
  // Try to resolve the platform-specific package via alias
  // This resolves the npm alias: @ngominhbinh708/spark-linux-x64 -> @ngominhbinh708/spark@VERSION-linux-x64
  const packageJsonPath = require.resolve(`${packageAlias}/package.json`);
  const vendorDir = path.join(path.dirname(packageJsonPath), "vendor");
  binaryPath = path.join(vendorDir, binaryName);
} catch (e) {
  // Package not installed
  console.error(`Platform package not found: ${packageAlias}`);
  console.error("");
  console.error("This may happen if:");
  console.error("  1. npm optionalDependencies was skipped due to a platform mismatch");
  console.error("  2. The package was not installed correctly");
  console.error("");
  console.error("Try reinstalling:");
  console.error("  npm install -g @ngominhbinh708/spark@latest");
  console.error("");
  console.error("Or check if your platform is supported:");
  console.error("  Platform: " + process.platform);
  console.error("  Arch: " + process.arch);
  process.exit(1);
}

// Check if binary exists
const fs = require("fs");
if (!fs.existsSync(binaryPath)) {
  console.error(`Binary not found: ${binaryPath}`);
  console.error("The platform package was installed but the binary is missing.");
  console.error("Try reinstalling:");
  console.error("  npm install -g @ngominhbinh708/spark@latest");
  process.exit(1);
}

// Execute the binary with all arguments
const result = cp.spawnSync(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

process.exit(result.status === null ? 1 : result.status);
