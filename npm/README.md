# spark npm wrapper

This package installs the `spark` CLI binary and exposes it as a global command.

## Binary naming convention

Upload release binaries with these names:

- `spark-darwin-amd64`
- `spark-darwin-arm64`
- `spark-linux-amd64`
- `spark-linux-arm64`
- `spark-windows-amd64.exe`
- `spark-windows-arm64.exe`

## Required configuration before publish

1. Update `package.json`:
- `name` (your npm package name)
- `repository.url` (already set to this repository by default)
- `version` (should match the binary release tag)

2. Ensure binaries are available in GitHub Releases under:
- `https://github.com/<owner>/<repo>/releases/download/v<version>/...`

3. During install, `bin/install.js` downloads from:
- `SPARK_BINARY_URL` (highest priority, direct file URL), or
- `SPARK_BINARY_BASE_URL` + `SPARK_BINARY_VERSION` + `SPARK_BINARY_NAME`

## Local test

```bash
cd agent-launch/npm
SPARK_BINARY_URL="file-or-http-url-to-your-binary" npm install
node bin/spark.js --help
```

## Publish

```bash
# 1) bump npm/package.json version (for example: 0.2.0)
# 2) create matching git tag and push
git tag v0.2.0
git push origin v0.2.0
```

`release.yml` will automatically:
- build/upload binaries to GitHub Releases
- publish `npm/` to npm registry

### One-command release (recommended)

From repository root:

```bash
scripts/release-npm.sh patch --push
```

This script will:
- verify current branch is `main`
- verify git working tree is clean
- run `npm version` in `npm/` (creates commit + tag `vX.Y.Z`)
- push `main` and tags to trigger GitHub Actions release

## GitHub Actions auto release

Repository workflow: `../.github/workflows/release.yml`

It runs on tags like `v0.1.0` and will:
1. Build and upload binaries with GoReleaser
2. Publish the npm package from `npm/`

Required setup:

1. Set `npm/package.json`:
- `version` must match the git tag (without leading `v`)
- `repository.url` must be a real GitHub repo URL

2. Add GitHub Actions secret:
- `NPM_TOKEN` (an npm automation token with publish permission)

3. npm package must not have this version already published (workflow now detects this and skips duplicate publish on reruns)

## User install

```bash
npm install -g <your-package-name>
spark
```
