#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const https = require("https");
const http = require("http");

const pkg = require("../package.json");

const platformMap = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows"
};

const archMap = {
  x64: "amd64",
  arm64: "arm64"
};

const platform = platformMap[process.platform];
const arch = archMap[process.arch];

if (!platform || !arch) {
  console.error(
    `Unsupported platform/arch: ${process.platform}/${process.arch}. ` +
      "Set SPARK_BINARY_URL to a direct binary URL for manual install."
  );
  process.exit(1);
}

const ext = platform === "windows" ? ".exe" : "";
const binaryName = process.env.SPARK_BINARY_NAME || `spark-${platform}-${arch}${ext}`;
const inferredReleaseBaseUrl = inferGitHubReleaseBaseUrl(pkg.repository);
const releaseBaseUrl =
  process.env.SPARK_BINARY_BASE_URL ||
  inferredReleaseBaseUrl ||
  "https://github.com/REPO_OWNER/REPO_NAME/releases/download";
const releaseVersion = process.env.SPARK_BINARY_VERSION || `v${pkg.version}`;
const directUrl = process.env.SPARK_BINARY_URL;
const url = directUrl || `${releaseBaseUrl}/${releaseVersion}/${binaryName}`;

if (!directUrl && releaseBaseUrl.includes("REPO_OWNER/REPO_NAME")) {
  console.error(
    "SPARK_BINARY_BASE_URL is not configured. Set it to your GitHub releases base URL,\n" +
      "for example: https://github.com/<owner>/<repo>/releases/download"
  );
  process.exit(1);
}

const distDir = path.join(__dirname, "..", "dist");
const outputName = `spark${ext}`;
const outputPath = path.join(distDir, outputName);

fs.mkdirSync(distDir, { recursive: true });

download(url, outputPath)
  .then(() => {
    if (platform !== "windows") {
      fs.chmodSync(outputPath, 0o755);
    }
    console.log(`spark installed: ${outputPath}`);
  })
  .catch((err) => {
    console.error(`Failed to install spark binary from: ${url}`);
    console.error(err.message);
    process.exit(1);
  });

function download(downloadUrl, destination) {
  return new Promise((resolve, reject) => {
    const client = downloadUrl.startsWith("https://") ? https : http;

    client
      .get(downloadUrl, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return resolve(download(res.headers.location, destination));
        }

        if (res.statusCode !== 200) {
          return reject(new Error(`HTTP ${res.statusCode}`));
        }

        const file = fs.createWriteStream(destination, { mode: 0o755 });
        res.pipe(file);

        file.on("finish", () => {
          file.close(resolve);
        });

        file.on("error", (err) => {
          fs.rmSync(destination, { force: true });
          reject(err);
        });
      })
      .on("error", reject);
  });
}

function inferGitHubReleaseBaseUrl(repository) {
  if (!repository) {
    return "";
  }

  const rawUrl = typeof repository === "string" ? repository : repository.url || "";
  if (!rawUrl) {
    return "";
  }

  const cleaned = rawUrl
    .replace(/^git\+/, "")
    .replace(/\.git$/, "")
    .replace(/^git@github\.com:/, "https://github.com/");

  const match = cleaned.match(/^https:\/\/github\.com\/[^/]+\/[^/]+$/);
  if (!match) {
    return "";
  }
  return `${cleaned}/releases/download`;
}
