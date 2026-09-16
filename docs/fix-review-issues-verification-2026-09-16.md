# 修复完成度核查（2026-09-16）

**结论：REQUEST CHANGES，尚未全部完成。置信度：高。**

对照 `docs/fix-review-issues-prompt.md` 检查当前工作区（包括暂存和未暂存修改），三个原始问题均已有修复代码和回归测试，但还存在三处可复现的遗漏。

## 清单完成情况

| 原任务 | 核查结果 |
| --- | --- |
| 注销路径穿越及管理接口保护 | 部分完成：HTTP provider 校验、注销 POST/DELETE 限制、随机管理令牌、回环地址与来源检查均已加入；存储符号链接防护和令牌文件权限仍有缺口。 |
| 刷新时保留旧 refresh token | 已完成：响应无新值时保留旧值，有新值时替换；包含本地模拟端点、持久化和再次刷新测试。 |
| Web 切换 provider 同步认证引用 | 部分完成：规范及兼容 AuthRef 会更新；省略 provider 保留账号绑定、API key 保留/清除及 HTTP 保存重载已有测试。但认证模式变化可能使旧 key 被用于新上游。 |

CLI 的现有 logout 路径直接调用认证存储，并不经过 daemon 管理 HTTP API；未发现必须改为携带 HTTP 管理令牌的现有 CLI 请求。新增 `ManagementRequest` 在管理接口测试中被使用。

## 必须补齐的发现

### P1：悬空符号链接仍可让 Save 写出认证根目录

位置：[internal/auth/auth.go:284](../internal/auth/auth.go#L284)，写入点为第 505 行。

`confinedPath` 在 `EvalSymlinks(abs)` 失败后，仅校验父目录，随后返回仍可能是符号链接的文件路径。若链接目标尚不存在，解析失败，但 `os.WriteFile` 会跟随链接并创建根目录之外的目标文件。

复现：在 `t.TempDir()` 中创建 `auth/claude/default.json -> ../根目录外的 outside.json`（测试使用绝对目标路径），目标文件最初不存在；调用 `Save` 保存假凭据，返回 nil，外部 `outside.json` 被创建并写入。这不满足任务要求的“其他路径拼接入口也必须受根目录约束”。

建议：对解析失败采用明确的错误分类和拒绝策略；用 `Lstat` 等识别悬空链接，不能把它当作普通不存在文件。安全写入应同时避免检查与写入之间重新跟随链接；补充悬空账号文件、扁平 provider 文件以及读取/枚举路径的链接回归测试。

### P1：已有 daemon.json 会继续以 0644 保存管理令牌

位置：[internal/compat/daemon/client.go:83](../internal/compat/daemon/client.go#L83)。

把 `os.WriteFile` 的 mode 从 0644 改为 0600，仅影响新建文件，不能收紧已有文件权限。升级前遗留的 daemon.json（例如旧进程异常退出后留下的文件）会继续以 0644 保存新增的管理令牌。在父目录允许遍历的环境中，其他本地用户可读取该凭据并调用管理接口。

复现：在临时 HOME 下预建并明确 chmod 为 0644 的 `.spark/daemon.json`，调用 `writeDaemonInfo` 写入假管理令牌，文件权限仍为 0644。

建议：使用权限为 0600 的临时文件原子替换，或在写入令牌前可靠地收紧已有文件权限并处理错误；增加旧文件权限迁移用例。

### P1：provider 切换把 auth 模式降为 auto，可启用旧 API key

位置：[internal/httpserver/server.go:932](../internal/httpserver/server.go#L932)，具体模式赋值在第 939 行；凭据回退见 [internal/compat/engine/credential.go:78](../internal/compat/engine/credential.go#L78)。

旧配置可以同时保存 API key 和 OAuth 引用，并通过 `Credential.Mode=auth` 禁止使用 API key。切换 provider 时，当前代码只要发现保留的 key 非空，就把模式改为 auto。新 provider 没有可用 OAuth 记录时，解析器会回退到旧 key，并将其作为新上游的请求凭据。

复现：旧 profile 为 codex、auth 模式、`codex:default`，同时存有假旧 key；经 DTO 切换为 claude，使用空的临时 auth store 调用实际凭据解析器。返回结果为 `source=profile.api_key`、value 为假旧 key，而不是拒绝使用旧凭据。现有测试仅验证引用更新及 key 保留，没有验证实际凭据解析结果。

建议：切换 OAuth provider 时保留或明确设定 auth 模式，避免仅因存在历史 key 就启用回退；API key 可以按约定继续保存在配置中，但不应被自动激活。为“新 provider 未登录/凭据不可用”的情形增加实际解析器回归测试。

## 验证结果与范围

- `go test ./... -count=1`：通过。首次沙箱内运行因不允许监听本地端口失败，获得授权后在沙箱外重新运行，全量通过。
- `web` 目录 `npm run typecheck`：通过。
- 临时补充三个独立回归用例，执行 `go test ./internal/auth ./internal/compat/daemon ./internal/httpserver -run '^TestReview' -count=1`：三个均按预期失败，分别确认上述问题。
- 临时测试仅操作 `t.TempDir()`、临时 HOME 和假凭据，没有请求真实供应商，没有操作真实认证数据；运行后已移除临时测试文件。
- 本次未修改业务代码、未提交或推送，仅新增本核查报告。
- 本次未运行 Web 构建；这三项修复没有 Web 源码变更，现有暂存嵌入资源属于整体工作区改动，不据此声称其构建一致性已验证。
- 无法仅从当前工作区证明上一位 agent 曾执行修复前失败验证；本次确认了现有测试通过及三个遗漏的独立失败复现。

建议先补齐以上三项及相应回归测试，再判定这份修复清单全部完成。
