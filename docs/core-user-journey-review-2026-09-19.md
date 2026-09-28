# Spark 核心使用主线分析

分析日期：2026-09-19。范围：当前工作区的 README、CLI/TUI、配置、Codex/Claude 启动隔离、连接探测、Web 配置入口与相关测试。未修改业务代码，未调用真实供应商、未运行真实 Agent 会话；下文区分代码确认的行为与需要端到端验证的风险。

核心判断：Spark 已经具备较完整的功能基础，但“配置完成 → 确认可用 → 稳定启动 → 保存成果 → 下次继续”还没有形成可靠闭环。当前更值得投入的是启动确定性、会话持久化与失败恢复。

核心用户目标应当是：配置一次供应商，选择 Agent 和模型，完成真实编码任务，退出后能继续，并能明确知道失败发生在哪一层。

| 主线阶段 | 当前主要问题 | 优先级 |
| --- | --- | --- |
| 首次进入 | 默认配置不能启动，缺少引导；未设置 Agent 时按名称排序兜底 | P1 |
| 验证配置 | 测试只验证上游非流式 HTTP 成功，不覆盖实际代理与工具调用 | P1 |
| 配置/启动 | Codex、Claude 的“仅配置”可能虚报完成 | P1 |
| 运行与退出 | 新建会话资产可能留在临时目录并被清理 | P1，优先排查 |
| 失败后重试 | 启动前就记历史，错误只输出到终端后返回菜单 | P2 |
| 日常快捷启动 | Agent 可来自上次使用，Profile/模型却来自全局默认 | P2 |
| Web 配置交接 | 保存、拉模型后缺少验证和进入终端使用的闭环 | P2 |

1. **会话持久化存在结构性缺口，应该最先验证和修复。**

   触发条件：首次使用某个 Agent，真实配置目录尚不存在，或其中还没有 sessions、projects、history 等资产。`symlinkEntries` 仅镜像启动前已有的条目；源目录不存在时直接成功返回。Agent 随后在临时 HOME 内新建的顶层资产不会自动连接到真实目录，退出时临时目录会被删除。即使已有安装，后来新增的顶层文件也存在相同风险。镜像降级为复制时，运行期修改同样缺少统一回写。

   用户影响：当次能工作，但下次恢复会话或查看历史时可能找不到记录。这是条件性数据丢失风险，不是对所有用户都会丢会话的判断。实际 Agent 写哪些路径，仍需真机验证。

   证据：[镜像仅遍历已有条目](../internal/integrations/util.go#L197)、[复制降级](../internal/integrations/util.go#L236)、[Codex 清理临时目录](../internal/integrations/codex.go#L119)、[Claude 清理临时目录](../internal/integrations/claude.go#L124)。Codex 当前退出时只有专门的 hook trust 回写，不能代替通用会话持久化。

   建议：明确区分临时凭据/配置与持久会话资产，为后者预建真实路径并稳定映射；对文件替换写入、复制降级、新增资产分别设计策略。验收应覆盖“空用户目录首次启动 → 写入会话 → 退出 → 再启动恢复”。

2. **“连接测试通过”与“选定 Agent 可用”之间差距较大。**

   目前测试直接请求上游，发送非流式 ping；多个端点中任何一个返回 2xx 就整体成功，并未验证响应内容结构。HTML 或业务错误体即使返回 200，也可以通过这个判定。正常 Codex 启动默认经过共享 daemon，这条链路并没有被该探测覆盖。

   用户影响：用户看到 OK 后仍可能在首次流式输出、工具调用、工具结果回传或代理路由处失败，难以判断该改模型、协议还是本地配置。现有探测有连通性价值，但不适合代表编码可用性。

   证据：[成功条件](../internal/probe/model_connection.go#L232)、[非流式探测请求](../internal/probe/model_connection.go#L318)、[Codex 实际 daemon 路由](../internal/integrations/codex.go#L89)。

   建议：区分“上游连通”“协议响应有效”“目标 Agent 链路可用”；增加经真实 gateway 的流式与一次 tool-call/tool-result 往返测试。针对选定的 Agent 展示对应结果，保留逐端点结果，避免总体 OK 掩盖部分失败。

3. **“仅配置”对不同 Agent 的语义不一致，存在虚报成功。**

   `spark config codex`、`spark config claude` 或对应 `launch --config` 进入统一流程。只有实现 `Editor` 的集成会先调用编辑器；Codex、Claude 的配置生成发生在 `Run*` 内。在实际运行之前，统一流程却显示“Config written”，用户选 Not now 就直接结束，此时没有执行这些 Agent 的配置生成。

   用户影响：以为已完成配置，随后直接运行原生 Agent，使用的仍可能是旧设置。CLI 的“Configure without launching”还会再弹是否启动的交互，行为也不够清晰。

   证据：[Editor 分支](../internal/app/cli.go#L912)、[仅配置确认及提前返回](../internal/app/cli.go#L956)、[真正调用 Runner](../internal/app/cli.go#L999)、[Codex 启动期配置生成](../internal/integrations/codex.go#L129)。

   建议：定义每个集成是否支持持久配置。支持则实际写入后才报告成功；仅支持启动注入的集成应明确说明，提供启动预览或拒绝不适用的 config 操作。验收要检查实际文件，而不只检查确认文案。

4. **首次启动缺少围绕“第一次成功运行”的引导。**

   默认 Profile 只有 OpenAI URL，没有模型和凭据。首页仍提供 Quick launch，缺模型时打印错误再回菜单。用户需要自行去 Manage profiles、设置模型、配置认证，再回来选择 Agent。没有默认 Agent 或历史时，快捷启动选择排序后的第一个集成，而不是用户明确选择或已安装的集成。

   用户影响：安装 Spark 后第一件事不是验证一次成功请求，而是理解 Profile、API 类型、默认模型、默认 Agent 之间的关系。只配置了模型也未必能快捷启动到预期 Agent。

   证据：[空默认配置](../internal/config/config.go#L451)、[快捷启动失败分支](../internal/app/cli.go#L495)、[默认 Agent 兜底](../internal/app/cli.go#L818)、[集成名称排序](../internal/integrations/registry.go#L43)。

   建议：未就绪时首页主动作改为“完成首次设置”，串起“选择已安装 Agent → 供应商/认证 → 模型 → 验证 → 启动”。已有用户保留快速路径。首页显示具体缺少的条件，而不是只有配置摘要。

5. **启动尝试、启动成功和失败恢复没有清晰分层。**

   `RecordLaunch` 和配置保存发生在 prompt 解析、Runner 调用与 Agent 安装检查之前。失败的启动以及用户取消实际运行的 config-only 操作也会写入最近启动历史。TUI 对启动错误的处理主要是打印 stderr，然后继续回到主菜单。

   用户影响：最近使用记录可能代表一次失败尝试；用户缺少就地修复、重试和查看诊断的入口。这里只确认历史写入时机及恢复行为，不把它描述成“成功率统计错误”，因为没有验证存在该统计。

   证据：[提前记录历史](../internal/app/cli.go#L951)、[后续 prompt 检查](../internal/app/cli.go#L990)、[错误处理](../internal/app/cli.go#L505)、[Codex 内部安装检查](../internal/integrations/codex.go#L63)。

   建议：先做本地预检，区分最近选择、启动尝试、实际启动记录；失败界面保留原来的 Agent/Profile/模型，提供修改配置、重试、查看日志。真正启动成功的记录需要进程启动回调，不能简单等到长时间运行的 Agent 退出后才写。

6. **Quick launch 的默认组合容易和用户“继续上次”的直觉冲突。**

   未指定默认 Agent 时可以使用 `History.LastSelection`，但 Profile 始终来自 `DefaultProfile`，模型来自该默认 Profile。上次使用 Claude + B Profile，下次快捷启动可能得到 Claude + 全局 A Profile。这是可确认的设计行为，不一定是实现 bug；当前菜单描述也说明了使用默认值，但组合来源仍不直观。

   证据：[快捷启动 Profile/模型来源](../internal/app/cli.go#L794)、[Agent 来源](../internal/app/cli.go#L818)。

   建议：提供明确分开的“按默认组合启动”和“重复上次组合”，展示完整三元组。不要只继承上次 Agent，却让其余选择隐式回到全局默认。重复组合也不等同于恢复 Agent 会话，界面应区分。

7. **Web 是配置入口，但配置到实际使用的交接还不完整。**

   当前 Web API 包含 Profile CRUD、设置默认值、拉取模型及 Prompt 配置，没有与 TUI 对等的模型连接测试或 Agent 就绪检查。浏览器保存后，用户仍要返回终端自行完成验证和启动。

   证据：[Web API 能力](../web/src/api.ts#L94)、[服务路由](../internal/httpserver/server.go#L84)。这属于产品闭环缺口，不要求浏览器直接执行本地 Agent，也不应为此贸然增加远程命令执行接口。

   建议：保存后显示“下一步”，提供连接测试、当前配置完整性结果，以及正确转义的可复制终端启动命令。明确 Web 修改的是哪些持久配置、会在下一次什么操作中生效。

建议执行顺序：先验证并解决会话持久化与 config-only 虚报；随后补充真实 Agent 路径的健康检查和失败恢复；再收敛首次引导、快捷启动组合与 Web 交接。新增 Agent、更多设置和展示能力可以排在主线可靠性之后。

验证记录：执行 `go test ./internal/app ./internal/config ./internal/integrations ./internal/tui ./internal/probe ./internal/httpserver`，六个包均通过，部分结果命中缓存。初次运行被沙盒的缓存写权限和测试监听端口限制阻止，获得授权后重跑通过，未将环境限制计为项目缺陷。本次没有执行全仓测试、浏览器操作或真实模型端到端测试，因此不对全部代理协议正确性作结论。
