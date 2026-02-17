# Spark 部署（发布）流程文档

本文档定义 `spark` 的标准发布流程（GitHub Release 二进制 + npm 包发布）。

## 1. 目标与产物

每次发布完成后应得到以下产物：
- GitHub Releases 中的多平台二进制（由 GoReleaser 生成）
- npm 包 `spark-agent-launcher` 的新版本

涉及工作流：
- `Release Please`: `.github/workflows/release-please.yml`
- `Release`: `.github/workflows/release.yml`

## 2. 前置条件

发布前必须满足：
1. 默认发布分支为 `main`
2. GitHub Actions 已启用
3. 已配置 Actions Secret：`NPM_TOKEN`（当前方案）
4. `npm/package.json` 中 `repository.url` 已配置为真实仓库地址

## 3. 标准发布流程（推荐）

### 步骤 1：功能代码合入 `main`

- 正常通过 PR 合并功能/修复。
- 合并后会触发 `Release Please` 工作流。

### 步骤 2：等待 Release PR

- `Release Please` 会自动创建或更新一个 Release PR。
- PR 中会更新版本与 `CHANGELOG.md`，并同步 `npm/package.json` 的版本号。

### 步骤 3：合并 Release PR

- 合并后会自动创建并推送 tag：`vX.Y.Z`。
- 该 tag 会触发 `Release` 工作流。

### 步骤 4：自动发布

`Release` 工作流执行：
1. 校验 tag 与 `npm/package.json` 版本一致
2. 运行测试 `go test ./...`
3. 用 GoReleaser 发布二进制到 GitHub Releases
4. 发布 npm 包（若该版本已存在则自动跳过）

## 4. 手动兜底流程（workflow_dispatch）

当自动触发失败、需要重跑发布时：
1. 打开 GitHub Actions -> `Release`
2. 点击 `Run workflow`
3. 输入已存在的 tag（例如 `v0.1.6`）
4. 执行并观察三个 Job：`Validate Release`、`Release Binaries`、`Publish npm`

说明：
- 手动流程不会创建新 tag，只会基于已有 tag 重新发布。

## 5. 旧流程（仅保留兼容）

仓库仍保留脚本：`scripts/release-npm.sh`

适用于不走 Release PR 的场景：
```bash
scripts/release-npm.sh patch --push
```

该脚本会：
1. 校验当前分支和工作区状态
2. 更新 `npm/package.json` 版本
3. 创建并推送 `vX.Y.Z` tag
4. 触发 `Release` 工作流

## 6. 发布后检查清单

发布完成后核对：
1. GitHub Release 是否包含以下命名格式的二进制  
   `spark-darwin-amd64` / `spark-darwin-arm64` / `spark-linux-amd64` / `spark-linux-arm64` / `spark-windows-amd64.exe` / `spark-windows-arm64.exe`
2. npm 上版本是否可查询：
   ```bash
   npm view spark-agent-launcher version
   ```
3. 安装验证：
   ```bash
   npm install -g spark-agent-launcher@<version>
   spark --help
   ```

## 7. 常见故障与处理

1. `Tag version must match npm/package.json version`
- 原因：tag 与 `npm/package.json` 版本不一致
- 处理：修正版本后重新走 Release PR 或重新打正确 tag

2. npm 发布 403 或 2FA 相关错误
- 原因：`NPM_TOKEN` 权限不足或策略不匹配
- 处理：更换为可发布的 automation token，并更新仓库 Secret

3. 已存在同版本 npm 包
- 现象：`Publish npm` 被自动跳过
- 处理：正常行为；若需重新发布请使用新版本号

## 8. 后续优化建议

后续可迁移到 npm Trusted Publishing（OIDC），逐步移除 `NPM_TOKEN`。
