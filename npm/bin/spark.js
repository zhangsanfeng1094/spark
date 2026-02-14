#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const cp = require("child_process");

const ext = process.platform === "win32" ? ".exe" : "";
const binPath = path.join(__dirname, "..", "dist", `spark${ext}`);

if (!fs.existsSync(binPath)) {
  console.error(
    "spark binary not found. Reinstall package or run with:\n" +
      "SPARK_BINARY_URL=<direct binary url> npm install -g <package>"
  );
  process.exit(1);
}

const result = cp.spawnSync(binPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

process.exit(result.status === null ? 1 : result.status);
