# 修复清单复核（2026-09-16，当前工作区）

**最新复核：APPROVE。Confidence: HIGH。原修复清单及上次发现的 P1 遗漏已补齐，本次未发现范围内新的阻塞问题。**

## 最新复核结果（后续修复后）

- `profileFromDTO` 切换 provider 时，只有请求明确提交非空替换 key 才启用 api_key 模式；继承的旧 key 保留在配置中但不再被启用。
- 新 provider 已登录时使用新 provider 的凭据，未登录时返回空凭据，不回退到旧 key；显式提交新 key 时使用新 key。
- 新增 `TestProfileFromDTO_SwitchProviderFromAPIKeyDoesNotSendOldKey` 和 `TestProfileUpdateSwitchesAPIKeyModeAwayFromOldKey`，覆盖函数入口、HTTP 更新后保存重载、已登录/未登录及显式替换 key，并调用实际凭据解析器断言结果。
- 本次重新执行 `go test ./... -count=1`：全部通过（使用已授权的沙箱外测试方式）。
- 本次重新执行 `web` 目录 `npm run typecheck`：通过。
- 此前路径限制、管理接口保护、悬空文件链接拒绝、daemon.json 权限迁移、refresh token 保留的代码及回归测试仍在，全量测试通过。
- 本次只更新核查报告，未修改业务代码，未提交或推送。没有执行真实供应商登录或请求；未运行 Web 构建，也不据此证明已有嵌入资源与源码一致。

以下保留上次复核记录，所述 P1 现已关闭，不代表当前代码状态。

本次按 `fix-review-issues-prompt.md` 核对当前代码、调用链及回归测试。旧版 `fix-review-issues-verification-2026-09-16.md` 的结论已部分过时，不能直接用于判断当前状态。

## 完成情况

| 要求 | 当前结果 |
| --- | --- |
| 注销路径穿越、存储路径限制及管理接口保护 | 原报告中的路径穿越已修复；HTTP provider 校验、管理令牌、回环来源限制及修改方法限制已加入。悬空文件链接增加 Lstat 拒绝与临时文件替换，daemon.json 改为 0600 临时文件原子替换，相关回归测试通过。 |
| 刷新响应缺少 refresh token 时保留旧值 | 已完成；覆盖旧值保留、新值轮换、EnsureFresh 持久化及再次刷新。 |
| Web 切换 provider 同步认证引用及实际凭据 | 部分完成；引用更新、指定账号保留、API key 保留/清除、HTTP 保存重载均有测试。auth 模式切换不再激活旧 key，但 api_key 模式仍有下述遗漏。 |

## P1：api_key 模式切换 provider 后仍使用旧上游的 key

位置：[internal/httpserver/server.go:936](../internal/httpserver/server.go#L936)。实际凭据解析：[internal/compat/engine/credential.go:57](../internal/compat/engine/credential.go#L57)。

`profileFromDTO` 在 provider 改变时，如果旧配置为 `CredentialModeAPIKey`，仍将新配置设为 api_key；DTO 没有提交新 key 时，又会保留旧 key。虽然两个 AuthRef 都已更新到新 provider，解析器的 api_key 分支完全不读取 AuthRef，直接返回保留的旧 key。因此仍不满足原任务“不得把旧 provider 的凭据发送给新 provider 的上游”的要求。

独立复现：

1. 旧配置为 codex，模式 api_key，key 为假值 `fake-old-provider-key`。
2. 经 `profileFromDTO` 改为 claude 和 Anthropic endpoint，不提交新 key。
3. 在 `t.TempDir()` 下的 auth store 保存假的 claude 登录凭据。
4. 调用实际 `engine.ResolveRequestCredentialWithStore`。
5. 得到 `provider=claude ref=claude:default mode=api_key`，解析结果仍为旧 key，`source=profile.api_key`；新的 claude 凭据被忽略。

临时回归测试 `TestReviewProviderSwitchFromAPIKey` 已执行并按预期失败，确认该行为。测试未调用真实上游，运行后已删除临时测试文件。

建议：provider 改变且没有明确提交替换凭据时，不应因旧配置的 api_key 模式继续启用旧 key。可以保留配置中的 key，但应使其处于非活动状态，并按新 provider 选择认证；如新 provider 无可用认证，应拒绝使用旧 key。若允许切换时明确提交新 API key，应单独处理该意图。补充旧 api_key 模式的函数级和 HTTP 保存重载回归测试，并验证新 provider 已登录、未登录两种情况下的实际解析结果。

## 验证与限制

- 当前已有测试：`go test ./... -count=1` 全部通过。首次沙箱运行因禁止监听本地测试端口而失败，经授权在沙箱外重跑成功。
- `web` 目录：`npm run typecheck` 通过。
- 独立遗漏复现：`go test ./internal/httpserver -run '^TestReviewProviderSwitchFromAPIKey$' -count=1` 失败，输出确认仍选择旧 API key。
- 本次只核查，没有修改业务代码，没有提交或推送；仅新增本报告，临时测试已移除。
- 未运行 Web 构建；本次核查涉及的修复没有 Web 源码变化，不据此宣称已有暂存嵌入资源与源码一致。
- 不能仅凭当前工作区证明其他 agent 曾完成修复前失败验证；以上测试结果均为本次实际执行所得。

上次建议为补齐上述 api_key 分支；最新修复及验收结果见本文开头。
