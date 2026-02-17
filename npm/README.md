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

Recommended:
1. Merge feature/fix PRs into `main`.
2. Wait for `Release Please` (`.github/workflows/release-please.yml`) to open/update the release PR.
3. Merge the release PR. It updates version files and creates tag `vX.Y.Z`.
4. `Release` (`.github/workflows/release.yml`) publishes binaries and npm automatically.

### One-command release (recommended)

Legacy/manual path (if you do not use release PR flow):

From repository root:

```bash
scripts/release-npm.sh
```

This script will:
- ask you to choose release type (`patch` / `minor` / `major` / `prerelease` / custom version)
- verify current branch is `main`
- verify git working tree is clean
- run `npm version` in `npm/` (creates commit + tag `vX.Y.Z`)
- ask whether to push `main` and tags (or pass `--push` to auto-push)

## GitHub Actions auto release

Repository workflow: `../.github/workflows/release.yml`

It runs on tags like `v0.1.0` and will:
1. Build and upload binaries with GoReleaser
2. Publish the npm package from `npm/`
3. It can also be triggered manually with `workflow_dispatch` by passing an existing tag

Required setup:

1. Set `npm/package.json`:
- `version` must match the git tag (without leading `v`)
- `repository.url` must be a real GitHub repo URL

2. npm auth for publish:
- current: `NPM_TOKEN` secret (automation token with publish permission)
- preferred target: npm Trusted Publishing (OIDC), then token can be removed

3. npm package must not have this version already published (workflow now detects this and skips duplicate publish on reruns)

## User install

```bash
npm install -g <your-package-name>
spark
```

## Troubleshooting

### npm publish returns `E403` with 2FA message

Use an npm token that can publish in CI:
- preferred: `Automation` token
- or granular token with package `publish` permission and `Bypass 2FA` enabled

Then update GitHub Actions secret `NPM_TOKEN` and publish a new version tag.

### npm install fails with `getaddrinfo EAI_AGAIN github.com`

This means DNS/network to GitHub is temporarily unavailable. Retry install, or set a reachable binary URL:

```bash
SPARK_BINARY_URL="https://<reachable-url>/spark-linux-amd64" npm install -g spark-agent-launcher
```

You can also set `SPARK_BINARY_BASE_URL` to your mirror release base URL.

### Windows mouse click behavior

The TUI click handling is tuned for Windows terminal mouse events (`MouseLeft`) and Unix-like terminals (`MouseRelease`).
If click behavior is abnormal, test first in Windows Terminal / PowerShell, then update to the latest release binary.
