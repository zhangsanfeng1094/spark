你正在 `/home/fxh/tools/spark` 项目中工作。请直接修复以下三个已确认的问题，完成必要的回归测试，不要只输出方案。

先阅读 `docs/project-review-2026-09-16.md`，并核对当前代码。工作区已有大量暂存及未暂存改动，这些属于用户正在进行的重构：保留它们，不要 reset、checkout 覆盖、批量格式化或提交无关修改。若部分问题已修复，验证后跳过对应修改。

## 1. P0：注销接口路径穿越，可删除认证目录之外的数据

相关位置：
- `internal/compat/daemon/daemon.go`：`setupRoutes`、`handleManagementLogout`、`handleManagementDynamic`。
- `internal/auth/auth.go`：`Store.Delete`、`DeleteAccount` 及其他接受 provider 并拼接存储路径的入口。

已确认的调用链：注销 handler 在 `auth.SpecFor(provider)` 失败时仍把原始 provider 传入存储层。`Store.Delete` 直接拼接路径并调用 `os.RemoveAll`。在临时目录中，`NewStore(authDir).Delete("../unrelated")` 会删除与 auth 同级的 unrelated 目录。管理接口目前也没有认证或请求方法限制。

要求：
1. 在 HTTP 层拒绝无效 provider，保留合法别名及现有合法账号引用的行为；不要让未知值继续进入删除操作。
2. 在存储层实施独立校验，覆盖删除及其他存在同类路径拼接风险的入口。拒绝路径穿越、分隔符和非法标识符，并确保删除目标受存储根目录约束；检查符号链接是否能绕过该约束。不得仅靠 HTTP 校验。
3. GET 不得执行注销或其他状态修改，明确允许的方法并返回正确状态码。为管理操作加入适合本地 daemon 的认证和来源校验，更新实际 CLI/其他管理调用方。先检查现有约定，优先复用已有机制；不能将公开 profile 名称当作管理凭证，也不能只靠 CORS 响应头防止跨站修改。
4. 保持正常的单账号注销、provider 注销和其他管理功能可用。不要无关地改变推理接口的认证协议。

验收测试：非法 provider、`..`、含分隔符的值不会删除或修改存储根目录外的任何文件；有效 provider 和账号可正常注销；未认证和不允许的方法无法触发修改；合法管理调用方仍正常工作。所有删除相关测试必须只在 `t.TempDir()` 的自建虚拟数据上运行，绝不操作真实用户目录。

## 2. P1：刷新响应未返回 refresh token 时丢失旧令牌

相关位置：`internal/auth/oauth/refresh.go` 的 `Refresh`、`EnsureFresh`，及 `internal/auth/oauth/oauth.go` 的 `buildAuth`。

已确认：旧认证含 refresh token，刷新响应仅包含 `access_token` 和 `expires_in` 时，`Refresh` 成功返回但新记录的 RefreshToken 为空。`EnsureFresh` 会将其持久化，导致下一次无法续期。

要求：响应包含非空 refresh token 时使用新值；否则保留旧值。保持账号、ID token、scope 等现有保留行为。避免扩大为无关 OAuth 重构。

验收测试：使用本地模拟 token endpoint，覆盖“不返回 refresh token 时保留旧值”“返回新 refresh token 时替换旧值”，并验证 `EnsureFresh` 的持久化结果及后续仍可刷新。不得调用真实供应商或使用真实令牌。

## 3. P1：Web 切换认证提供商后仍使用旧认证引用

相关位置：`internal/httpserver/server.go` 的 `profileFromDTO`，以及 profile DTO、Web 调用方、`config.Profile.EffectiveAuthRef()` 和凭据解析代码。

已确认：旧 profile 为 `AuthProvider: codex`、`Credential.AuthRef: codex:default`，经 DTO 改为 `AuthProvider: claude` 后，返回值的 provider 虽为 claude，`EffectiveAuthRef()` 仍为 codex:default。原因是复制旧 Credential/AuthRef 后，仅在引用为空时才生成新引用。

要求：
1. 显式区分 provider 被修改和普通字段更新。切换 provider 时同步规范字段 `Credential.AuthRef`、兼容字段 `AuthRef` 及适用的认证模式，使实际使用的凭据与用户选择一致。
2. provider 未变化时保留已有的指定账号绑定，不能把自定义账号重置成 default。
3. 明确处理 DTO 中字段省略、空值及现有 API key 保留/清除约定；必要时调整 DTO 和调用方，但不要凭猜测扩大产品行为。
4. 不得把旧 provider 的凭据发送给新 provider 的上游。

验收测试：codex 切换到 claude 后有效引用为新 provider；仅改模型或 URL、或省略 provider 时保留原绑定；同 provider 的指定账号不丢失；API key 保留和显式清除行为不回退。除函数级测试外，通过现有 HTTP 测试入口验证更新、保存、重新加载后的结果。

## 工作方式与最终验收

- 先核对仓库说明、现有调用链和测试风格，然后实施修复。优先最小而完整的改动，不要修改无关第三方 Bifrost 代码。
- 新增真正覆盖上述失败场景的回归测试；先确认用例在修复前能够捕获问题，再验证修复后通过。
- 运行受影响包测试及 `go test ./...`；在 `web` 目录运行 `npm run typecheck`。如改动 Web 源码，按照仓库现有约定检查构建及嵌入资源是否需要同步。
- 遇到环境限制时如实说明，不得将未运行的检查写成通过。不要对真实服务发起登录、注销或删除操作。
- 不要提交或推送 git。完成后说明每个问题如何修复、涉及哪些文件、运行了哪些验证及结果，并列明仍存在的限制。
