# 项目问题检查（2026-09-16）

结论：REQUEST CHANGES。以下三个问题均通过临时回归用例复现，置信度高。建议先处理注销接口的数据删除风险，再修复认证续期和配置切换。

## 检查范围与验证

基于当前工作区（包含暂存及未暂存改动），重点检查本轮 Bifrost、daemon、认证及配置重构。此次不是整个项目及第三方依赖的穷尽审计。

- `go test ./...`：所有主模块包通过。
- `web` 目录 `npm run typecheck`：通过。
- 为下列三个问题分别编写临时回归用例，执行 `go test ./internal/auth ./internal/auth/oauth ./internal/httpserver -run '^TestReview' -count=1`，三个用例均按预期失败，证实问题存在。测试后已移除临时文件，未修改业务代码。
- 删除测试只操作 `t.TempDir()` 内自建的虚拟目录，没有操作真实用户数据；刷新测试使用本地模拟服务，没有请求真实供应商。
- 未执行真实供应商 OAuth 登录、浏览器交互、竞态检测或第三方 Bifrost 子模块的独立全量测试。

## 问题列表

| 优先级 | 问题 | 位置 |
| --- | --- | --- |
| P0 | 注销接口接受路径穿越参数，可删除认证目录之外的数据 | `internal/compat/daemon/daemon.go:254`；`internal/auth/auth.go:383` |
| P1 | 刷新响应不返回 refresh token 时，旧 refresh token 被清空 | `internal/auth/oauth/refresh.go:42` |
| P1 | Web 修改认证提供商后，实际凭据仍绑定旧提供商 | `internal/httpserver/server.go:922` |

### P0：注销接口存在路径穿越删除

`handleManagementLogout` 从查询参数或 JSON 读取 `provider`。`auth.SpecFor(provider)` 校验失败时没有拒绝请求，而是继续将原始输入传给 `store.Delete`。`Delete` 把该值直接拼接进文件路径，并调用 `os.RemoveAll` 删除整个目录，没有保证路径位于认证存储目录内。

复现：在临时根目录下创建 `auth/` 和 `unrelated/keep.txt`，执行 `NewStore(authDir).Delete("../unrelated")`。结果返回 nil，且 `unrelated/` 被删除。该参数可通过注销 handler 原样到达此函数。

管理路由目前没有认证或请求方法限制，GET 也能触发删除。默认监听回环地址限制了直接网络访问范围，但不能消除本机调用及跨站触发的风险；这里无需读取响应就会产生删除副作用。

建议：对管理接口增加认证、方法和来源校验；未知 provider 立即返回 400。在存储层独立验证 provider 为合法标识符，拒绝路径分隔符和 `.`、`..`，并在任何删除操作前确认目标路径位于存储根目录内。普通注销只删除确定的认证记录，不应允许请求参数决定任意递归删除目标。

### P1：刷新成功后可能失去后续续期能力

`Refresh` 使用 `buildAuth(spec, tr, a.Kind)` 创建新记录，而 `buildAuth` 直接将响应中的 `RefreshToken` 写入新记录。后续代码保留了旧 Account、IDToken 等字段，却没有在新 refresh token 为空时保留旧值。`EnsureFresh` 随后把这份记录持久化，覆盖原来的有效刷新令牌。

复现：模拟刷新接口只返回 `access_token` 和 `expires_in`，旧认证包含 `RefreshToken: "keep-me"`。`Refresh` 成功返回，但结果中的 RefreshToken 为空。下一次 access token 到期后，自动续期会认为没有刷新凭据，要求重新登录。

建议：仅在响应提供非空 refresh token 时替换旧值，否则沿用 `a.RefreshToken`；补充“不返回新刷新令牌”的续期及持久化回归测试。

### P1：Web 认证提供商与实际认证引用不一致

`profileFromDTO` 接收新的 `AuthProvider` 后，仍完整复制旧的 `Credential` 和 `AuthRef`。随后仅在认证引用为空时才根据新 provider 生成引用，因此原有引用不会更新。凭据解析优先使用 `Credential.AuthRef`，导致界面配置与实际发送的凭据不一致。

复现：旧 profile 的 provider 为 `codex`，`Credential.AuthRef` 为 `codex:default`；通过 DTO 将 provider 改成 `claude`。返回 profile 的 `AuthProvider` 是 `claude`，但 `EffectiveAuthRef()` 仍为 `codex:default`。

影响：请求可能携带旧提供商的凭据，导致认证失败；如果同时修改了上游地址，还可能把旧凭据发送给新的上游。

建议：显式识别认证 provider 的变化，同步更新 `Credential.AuthRef`、兼容字段 `AuthRef` 及适用的认证模式；只有 provider 未变化时才保留原有的指定账号绑定。补充切换 provider 和账号绑定的回归测试。
