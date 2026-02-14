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
- `repository.url`
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
cd agent-launch/npm
npm login
npm publish --access public
```

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

## User install

```bash
npm install -g <your-package-name>
spark
```
