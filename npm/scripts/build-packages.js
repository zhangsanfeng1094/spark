#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const https = require("https");
const http = require("http");

// Configuration
const VERSION = process.env.npm_package_version || require("../package.json").version;
const REPO_OWNER = "zhangsanfeng1094";
const REPO_NAME = "spark";
const PACKAGE_NAME = "@ngominhbinh708/spark";

// 支持本地测试：设置 SPARK_BINARY_BASE_URL=file:///path/to/binaries
const BASE_URL = process.env.SPARK_BINARY_BASE_URL || 
                 `https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download`;

const PLATFORMS = [
  { name: "linux-x64", os: "linux", cpu: "x64", binary: "spark-linux-amd64" },
  { name: "linux-arm64", os: "linux", cpu: "arm64", binary: "spark-linux-arm64" },
  { name: "darwin-x64", os: "darwin", cpu: "x64", binary: "spark-darwin-amd64" },
  { name: "darwin-arm64", os: "darwin", cpu: "arm64", binary: "spark-darwin-arm64" },
  { name: "windows-x64", os: "win32", cpu: "x64", binary: "spark-windows-amd64.exe" },
  { name: "windows-arm64", os: "win32", cpu: "arm64", binary: "spark-windows-arm64.exe" },
];

const PACKAGES_DIR = path.join(__dirname, "..", "packages");
const MAIN_PKG_PATH = path.join(__dirname, "..", "package.json");

function download(url, dest) {
  return new Promise((resolve, reject) => {
    // 支持本地文件 (file:// 协议)
    if (url.startsWith("file://")) {
      const srcPath = decodeURIComponent(url.slice(7));
      console.log(`  Copying from local: ${srcPath}`);
      
      try {
        fs.copyFileSync(srcPath, dest);
        resolve();
      } catch (err) {
        reject(err);
      }
      return;
    }

    const client = url.startsWith("https://") ? https : http;
    const file = fs.createWriteStream(dest);
    
    client.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        file.close();
        fs.unlinkSync(dest);
        return resolve(download(res.headers.location, dest));
      }
      
      if (res.statusCode !== 200) {
        file.close();
        fs.unlinkSync(dest);
        return reject(new Error(`HTTP ${res.statusCode}`));
      }
      
      res.pipe(file);
      file.on("finish", () => {
        file.close(resolve);
      });
    }).on("error", (err) => {
      file.close();
      fs.unlinkSync(dest);
      reject(err);
    });
  });
}

function generatePackageJson(platform, version) {
  return {
    name: PACKAGE_NAME,
    version: `${version}-${platform.name}`,
    description: `Spark CLI binary for ${platform.os} ${platform.cpu}`,
    license: "MIT",
    os: [platform.os],
    cpu: [platform.cpu],
    files: ["vendor"],
    repository: {
      type: "git",
      url: `https://github.com/${REPO_OWNER}/${REPO_NAME}.git`
    }
  };
}

// 更新主包的 optionalDependencies
function updateMainPackageJson() {
  const pkgJson = JSON.parse(fs.readFileSync(MAIN_PKG_PATH, "utf8"));
  
  // 更新版本号
  pkgJson.version = VERSION;
  
  // 更新 optionalDependencies
  const optionalDependencies = {};
  for (const platform of PLATFORMS) {
    optionalDependencies[`${PACKAGE_NAME}-${platform.name}`] = 
      `npm:${PACKAGE_NAME}@${VERSION}-${platform.name}`;
  }
  pkgJson.optionalDependencies = optionalDependencies;
  
  // 写回主包
  fs.writeFileSync(MAIN_PKG_PATH, JSON.stringify(pkgJson, null, 2) + "\n");
  
  console.log("\n========================================");
  console.log("Updated main package.json");
  console.log("========================================");
  console.log(`  Version: ${VERSION}`);
  console.log(`  optionalDependencies:`);
  for (const platform of PLATFORMS) {
    console.log(`    ${PACKAGE_NAME}-${platform.name}: npm:${PACKAGE_NAME}@${VERSION}-${platform.name}`);
  }
}

async function buildPlatformPackage(platform) {
  console.log(`\nBuilding ${platform.name}...`);
  
  const pkgDir = path.join(PACKAGES_DIR, platform.name);
  const vendorDir = path.join(pkgDir, "vendor");
  const binaryName = platform.os === "win32" ? "spark.exe" : "spark";
  const binaryPath = path.join(vendorDir, binaryName);
  
  // Create directories
  fs.mkdirSync(vendorDir, { recursive: true });
  
  // Generate package.json (dynamically, like Codex)
  const pkgJson = generatePackageJson(platform, VERSION);
  const pkgJsonPath = path.join(pkgDir, "package.json");
  fs.writeFileSync(pkgJsonPath, JSON.stringify(pkgJson, null, 2) + "\n");
  console.log(`  Generated: package.json`);
  
  // Determine download URL
  let downloadUrl;
  if (BASE_URL.startsWith("file://")) {
    // 本地测试模式
    downloadUrl = `${BASE_URL}/${platform.binary}`;
  } else {
    // GitHub Releases 模式
    downloadUrl = `${BASE_URL}/v${VERSION}/${platform.binary}`;
  }
  
  console.log(`  Downloading from: ${downloadUrl}`);
  
  try {
    await download(downloadUrl, binaryPath);
    
    // 检查文件大小
    const stats = fs.statSync(binaryPath);
    console.log(`  Downloaded: ${binaryPath} (${(stats.size / 1024).toFixed(1)} KB)`);
    
    // Set executable permission on Unix
    if (platform.os !== "win32") {
      fs.chmodSync(binaryPath, 0o755);
    }
  } catch (err) {
    console.error(`  Failed to download: ${err.message}`);
    
    // 本地测试时，如果文件不存在，创建一个占位符
    if (BASE_URL.startsWith("file://")) {
      console.log(`  Creating placeholder binary for testing...`);
      fs.writeFileSync(binaryPath, "#!/bin/sh\necho 'spark placeholder'\n");
      if (platform.os !== "win32") {
        fs.chmodSync(binaryPath, 0o755);
      }
    } else {
      throw err;
    }
  }
  
  console.log(`  Package: ${PACKAGE_NAME}@${VERSION}-${platform.name}`);
}

async function main() {
  console.log("========================================");
  console.log("Building platform packages");
  console.log("========================================");
  console.log(`Package: ${PACKAGE_NAME}`);
  console.log(`Version: ${VERSION}`);
  console.log(`Base URL: ${BASE_URL}`);
  console.log("========================================");
  
  // Clean packages directory
  if (fs.existsSync(PACKAGES_DIR)) {
    fs.rmSync(PACKAGES_DIR, { recursive: true });
  }
  fs.mkdirSync(PACKAGES_DIR, { recursive: true });
  
  // Build each platform package
  for (const platform of PLATFORMS) {
    await buildPlatformPackage(platform);
  }
  
  // 更新主包的 optionalDependencies
  updateMainPackageJson();
  
  console.log("\n========================================");
  console.log("✓ All platform packages built successfully!");
  console.log("========================================");
  
  console.log("\nPublishing commands:");
  console.log("  # Platform packages:");
  for (const platform of PLATFORMS) {
    console.log(`  cd npm/packages/${platform.name} && npm publish --access public --tag ${platform.name}`);
  }
  console.log("  # Main package:");
  console.log("  cd npm && npm publish --access public");
}

main().catch((err) => {
  console.error("Failed to build packages:", err);
  process.exit(1);
});
