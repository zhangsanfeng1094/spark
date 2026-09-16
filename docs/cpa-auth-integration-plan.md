# Spark 接入 CLIProxyAPI 式 Auth 登录 — 设计方案

> 目标：让 Spark（TUI + daemon 兼容代理）具备 CLIProxyAPI（`router-for-me/CLIProxyAPI`，下称 CPA）那种
> "登录态"认证能力——即用浏览器 OAuth 授权码 + PKCE、以及 Device Flow，托管各家官方 CLI
> （Claude Code / Codex / Gemini / Grok）的上游登录 token，并把 token 注入到启动的官方 CLI / 代理请求里，
> 而不是现在的"只在配置里手写 API key（BYOK）"。

---

## 1. 现状与差距

### 1.1 CPA 的认证架构（要抄的三层）

| 层 | 做什么 | CPA 实现 |
|---|---|---|
| **上行 OAuth 登录** | 拿到各家上游的登录态 | 浏览器 **Authorization Code + PKCE**（Claude 回调端口 54545、Codex 1455，Antigravity/AI Studio）；**Device Flow**（Kimi 专有、Codex 兜底、xAI）。`AuthManager.Login()` 起本地回调 server、开浏览器、换 token，存成 `Auth` 记录到 `AuthDir` |
| **下行 client 鉴权** | client 连代理时校验 | `sdkaccess.Manager.Authenticate()` 链式跑多个 provider，任一成功即放行；内置 `config-api-key` 从 `Authorization: Bearer` / `X-Goog-Api-Key` / `X-Api-Key` / `?key=` / `?auth_token=` 提取并校验 |
| **请求级凭证注入** | 每个请求选哪套上游账号并注入 | executor（如 `claude_executor.go::PrepareRequest`）按命中账号注入：OAuth 账号用 `Bearer`，API key 用 `x-api-key`，并复刻原生 client wire 头 |

### 1.2 Spark 现状

- **代理框架已同构**：daemon 默认 `127.0.0.1:42337`，暴露 `/v1/messages`(Claude)、`/v1/responses`(Codex)、`/v1/...generateContent`(Gemini)；`ResolveProfileFromRequest` 已做 profile 路由（`X-Spark-Profile`、`spark-profile:`/`spark-key:` token、`x-api-key`、`?key=`）。
- **登录态缺位**：目前是 **BYOK**——`internal/integrations/` 各 runner（`claude.go`、`codex.go`、`grok.go`）在启动官方 CLI 时，刻意**隔离**各家真实登录态（`.claude/.auth*`、`.codex/auth.json*`、`.grok/auth.json`），只用 `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` / `OPENAI_API_KEY` / `CODEX_API_KEY` 注入 API key。
  - `claude.go::shouldSkipClaudeMirror`：跳过 `.auth*`
  - `codex.go::shouldSkipCodexMirror`：跳过 `auth.json*`
  - `grok.go` 注释：隔离登录态使 Spark 启动是 BYOK-only，`~/.grok/auth.json` 的 OAuth JWT 会 401。

**结论**：Spark 缺的不是代理，而是 CPA 的**第一层（OAuth 登录）**和**第三层（登录态注入）**。第二层（下行鉴权）已有雏形，可升级为链式。

---

## 1.3 与 Bifrost Core 的关系（不冲突，正交分层）

Spark 当前已按 `docs/bifrost-integration-plan.md` 用 **Bifrost Core**（`github.com/maximhq/bifrost/core`）替代自研转发层，本方案**不触碰 Bifrost 引擎本身**，两者是正交的两层：

| | Bifrost Core（现状） | CPA-auth 子系统（本方案） |
|---|---|---|
| 角色 | **data plane**：上游协议映射、HTTP 发送、流式归一化、重试 | **credential source**：登录态的获取、存储、刷新 |
| 关心 | "请求发给谁、怎么发" | "凭证从哪来、用哪套账号" |
| 凭证接口 | `SparkAccount.GetKeysForProvider` + `BifrostContextKeyDirectKey` | 解析出 `schemas.Key` 后喂给 Bifrost |

**凭证注入链（已存在于 `internal/compat/ingress/*/handler.go`）：**
```go
apiKey := activeProfile.EffectiveAPIKey()          // ← 本方案要替换的"凭证来源"
h.engine.ConfigureProvider(provider, cleanedBase, apiKey)  // 配到 SparkAccount
engine.BindDirectKey(bCtx, bReq.Provider, apiKey)          // 绑到 BifrostContext
```
接入登录态**只需把 `apiKey` 换成 `resolveRequestCredential(...)` 的结果**（登录态 access_token 或 API key）。Bifrost 拿到什么 `Key` 就用什么，完全不感知凭证来源。**因此接入 auth 不会与 Bifrost 冲突，也不需要改 `engine/bifrost.go`、`engine/profile.go`。**

**两条并行路径都要接登录态（互不干扰）：**
1. **代理路径**（daemon + ingress）：`handler.go` 的凭证注入点改一处即可（见 4.3）。
2. **直连启动路径**（TUI 直接启动官方 Claude/Codex/Grok CLI）：`integrations/*.go` 注入环境变量 / 登录态文件（见 4.4）。

> 若接入过程中 `MapProfileToProvider`/`BindDirectKey` 遇到上游要求"登录态用 Bearer、key 用 x-api-key"的差异化 wire 头，是在 handler 层做适配，同样不动 Bifrost 核心。

---

## 2. 目标架构

```
┌──────────────────────────────────────────────────────────────────────┐
│  Spark daemon (127.0.0.1:42337)                                       │
│                                                                       │
│   /v1/messages · /v1/responses · /v1/*generateContent                │
│        │   ▲                                                            │
│        ▼   │ 请求级凭证注入 (OAuth Bearer / x-api-key)                │
│   ┌───────────────┐   ┌──────────────────────────┐                    │
│   │ profile 路由   │──▶│ AuthResolver             │                    │
│   │ ResolveProfile │   │  key ↔ OAuth 登录态 决策  │                    │
│   └───────────────┘   └────────────┬─────────────┘                    │
│                                    ▼                                  │
│   ┌─────────────────────────────────────────────────────────────┐     │
│   │ auth 子系统（新）                                              │     │
│   │  AuthStore  → ~/.spark/auth/<provider>.json   (Auth 记录)    │     │
│   │  Authenticator: OAuth PKCE / Device Flow / 刷新              │     │
│   │  /v0/management/{provider}-auth-url (TUI 用)                 │     │
│   └─────────────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────────────┘
        ▲                            ▲
        │ 注入 env                   │ 浏览器 / 回调
┌───────┴──────────┐        ┌───────┴───────────────┐
│ 官方 CLI 启动环境  │        │ OAuth 授权服务器        │
│ ANTHROPIC_*      │        │ (Anthropic/OpenAI/...) │
│ OPENAI_*/CODEX_* │        └────────────────────────┘
└──────────────────┘
```

---

## 3. 数据模型与存储

### 3.1 Auth 记录（新文件 `internal/auth/auth.go`）

```go
package auth

// ProviderKind 区分上游登录方式
type ProviderKind string
const (
    KindOAuth  ProviderKind = "oauth"   // Authorization Code + PKCE
    KindDevice ProviderKind = "device"  // Device Flow
    KindAPIKey ProviderKind = "apikey"  // 透传配置里手写的 key（现状）
)

// Auth 一份上游账号的登录态（对应 CPA 的 Auth 记录）
type Auth struct {
    Provider    string       `json:"provider"`               // "claude" | "codex" | "gemini" | "grok"
    Kind        ProviderKind `json:"kind"`
    AccessToken string       `json:"access_token,omitempty"`
    RefreshToken string      `json:"refresh_token,omitempty"`
    ExpiresAt   time.Time    `json:"expires_at,omitempty"`
    // OAuth 元信息
    ClientID    string       `json:"client_id,omitempty"`
    Scope       string       `json:"scope,omitempty"`
    // 当前登录的账号标识（展示用）
    Account     string       `json:"account,omitempty"`
}

type Store struct {
    dir string // ~/.spark/auth
}

func (s *Store) Get(provider string) (*Auth, error)
func (s *Store) Save(a *Auth) error
func (s *Store) Delete(provider string) error
func (s *Store) List() ([]*Auth, error)
```

- 存储目录：`~/.spark/auth/<provider>.json`（对齐 `configDir()` = `~/.spark`）。权限 `0o600`（含 token）。
- **profile 与登录态的绑定**：不新增顶层字段，沿用现有约定——profile 命中哪家（按 base_url / api_type / default_model 判别，见 4.3），就用同 provider 的登录态。用户显式想用某个登录账号时，可在 profile 增加可选 `auth_provider` 字段（见 3.2 config.Profile 扩展）。

### 3.2 config.Profile 扩展（`internal/config/config.go`）

```go
type Profile struct {
    // ...现有字段...
    AuthProvider string `json:"auth_provider,omitempty"` // 显式指定用哪个登录 provider；空=按判别规则自动选
}
```

`EffectiveAPIKey()` 保持不变（兼容 BYOK）；新增一个解析层（4.3）决定"该 profile 的请求凭证 = 登录态 or API key"。

---

## 4. 子系统设计

### 4.1 上行 OAuth 登录（新包 `internal/auth/oauth`）

**登录入口**（CLI 命令 + TUI 面板，见 5）：

- `spark login --provider claude` / `--provider codex` / `--provider gemini`
- `spark login --provider codex --device`（强制 Device Flow）
- TUI：新增"登录"面板，复用现有 TUI 组件（参考 `internal/tui/` 下 profile/mcp manager 的 form + list 结构）。

**OAuth Authorization Code + PKCE 流程**（对应 CPA `DoClaudeLogin`/`DoCodexLogin` + 本地回调 server）：

```go
// internal/auth/oauth/flow.go
type Flow struct {
    Provider string
    NoBrowser bool
    CallbackPort int // claude:54545, codex:1455, gemini:配置
    Handler func(a *auth.Auth) error // 收到 token 后交给 Store.Save
}

func (f *Flow) Run(ctx context.Context) (*auth.Auth, error) {
    // 1. 生成 code_verifier + state（PKCE），存 verifier 到内存/临时文件
    // 2. 起本地回调 http server 监听 CallbackPort
    // 3. 拼 authorization_url：?client_id&redirect_uri=http://127.0.0.1:PORT&code_challenge&state
    // 4. 有浏览器则 browser.OpenURL，否则打印 URL 让用户手动访问
    // 5. 收到 /callback?code=..&state=..  → 校验 state → POST /token 换 access_token+refresh_token
    // 6. 写 Auth 记录，回调 server 关闭
}
```

**Device Flow**（对应 CPA Kimi/Codex 兜底）：

```go
// internal/auth/oauth/device.go
func (f *Flow) Device(ctx context.Context) (*auth.Auth, error) {
    // 1. POST /device_authorization → device_code, user_code, verification_uri, interval
    // 2. 打印 user_code + URL（或 TUI 渲染二维码）
    // 3. 轮询 POST /token（间隔 interval），成功后返回 Auth
}
```

**各 provider 端点参数**集中在 `internal/auth/oauth/providers.go`（client_id、auth 端点、token 端点、scope、回调端口、device 端点）。Claude/Codex 的具体 client_id 与端点需按 CPA 仓库 `internal/cmd/anthropic_login.go` / `openai_login.go` 及上游公开端点对齐。

**token 刷新**（`internal/auth/oauth/refresh.go`）：`Store.Get` 返回的 `Auth` 若 `ExpiresAt` 临近，用 `RefreshToken` 换新，写回 Store。

### 4.2 下行 client 鉴权：升级为链式（`internal/compat/daemon/daemon.go`）

把现有 `applyAuthToken` 从单函数升级为 CPA 式 provider 链，放在 `ResolveProfileFromRequest` 之前，作为请求鉴权/路由的统一入口：

```go
type authProvider interface {
    Identifier() string
    Authenticate(r *http.Request) (principal string, err error)
}

// 注册顺序即优先级：
//  - spark-profile / spark-key token  → profile 选择（现状保留）
//  - config-api-key（新）            → 校验 daemon 自身 API key（对齐 CPA config-api-key）
//  - oauth-account（新）             → 命中某 provider 登录态，principal=账号
func (s *Server) authenticateRequest(r *http.Request) (principal string, err error) {
    for _, p := range s.authProviders {
        if got, err := p.Authenticate(r); err == nil {
            return got, nil
        }
    }
    return "", errNoCredentials
}
```

`config-api-key` provider 从 `Authorization: Bearer` / `X-Goog-Api-Key` / `X-Api-Key` / `?key=` / `?auth_token=` 提取，与 `~/.spark/config.json`（或新 `~/.spark/daemon-keys.json`）里配置的 key 校验——这正好覆盖"远程连 daemon"时放行的场景（CPA 支持把 daemon 暴露给非本机 client）。

### 4.3 请求级凭证注入（改 `internal/compat/ingress/*/handler.go`，不动 Bifrost 引擎）

凭证注入点不在 `daemon.go` 的路由层，而在**各 ingress handler 的 `EffectiveAPIKey()` 调用处**——那里才是真正决定发给上游的凭证的地方。以 `ingress/codex/handler.go` 为例，接入前后对照：

```go
// 现状：直接取 profile 的 key
apiKey := activeProfile.EffectiveAPIKey()
h.engine.ConfigureProvider(provider, cleanedBase, apiKey)
engine.BindDirectKey(bCtx, bReq.Provider, apiKey)

// 接入后：换成 resolveRequestCredential，其余（ConfigureProvider / BindDirectKey）不变
cred := resolveRequestCredential(activeProfile, r)   // → credential{Scheme, Value, Source}
h.engine.ConfigureProvider(provider, cleanedBase, cred.Value)
engine.BindDirectKey(bCtx, bReq.Provider, cred.Value)
```

`resolveRequestCredential(profile, r) (cred)` 决定用登录态还是 API key：

```go
// 优先级：
//  1. 请求头已带 spark-key / x-api-key（现状，显式 BYOK）
//  2. profile.AuthProvider 显式指定 → 用该 provider 登录态
//  3. 自动判别：profile 指向哪家上游（MapProfileToProvider 判定，见 engine/profile.go）
//     → 该 provider 有登录态则用登录态，否则回退 profile.api_key
type credential struct {
    Scheme  string // "bearer" | "x-api-key"
    Value   string
    Source  string // "oauth.<provider>" | "profile.api_key" | "env.*"
}
```

`resolveRequestCredential` 可放公共位置（如 `internal/compat/engine/profile.go` 旁新增 `credential.go`），三个 ingress handler（codex / claude / gemini）复用。claude / gemini 的 handler 同样替换其 `EffectiveAPIKey()` 调用。

**上游注入语义**（对齐 CPA executor）：
- **Claude**：OAuth → `Bearer`（`Authorization: Bearer <access>`）；API key → `x-api-key`。
- **Codex**：OAuth → `Bearer`；API key → `Authorization: Bearer <key>`（OpenAI Responses 走 Bearer）。
- **Gemini**：一律 `x-goog-api-key`（或用登录态的 OAuth 头，按上游支持情况）。
- **Grok**：现有 `grok.go` 已处理 `spark-*` 模型用 `SPARK_GROK_API_KEY` 注入；要支持 OAuth 登录态时，需让 `writeGrokLaunchHome` 不再隔离 `auth.json`（或改为链接一份 token 化的 auth.json）。

### 4.4 官方 CLI 启动时的登录态注入（改 `internal/integrations/*.go`）

现在 `claude.go`/`codex.go` 启动 CLI 时用 `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` 注入 key，并**隔离**真实登录态。新增逻辑：当该 profile 命中登录态时——

- **Claude**（`claude.go`）：
  - 注入 `ANTHROPIC_AUTH_TOKEN=<access_token>`（OAuth 登录态走这个，Claude Code 支持 Bearer token）。
  - 或写入登录态文件：`shouldSkipClaudeMirror` 不再跳过 `.auth*`，改为把 `~/.claude/.credentials.json` 的 token 字段替换成 Spark 登录态 token。
- **Codex**（`codex.go`）：`shouldSkipCodexMirror` 当前跳过 `auth.json`；登录态模式改为把 `~/.codex/auth.json` 的 `OPENAI_API_KEY` 令牌替换成登录态 token。
- **Grok**（`grok.go`）：登录态模式让 `writeGrokLaunchHome` 把 `~/.grok/auth.json` 链接进 launch home。

> 安全注记：真实登录态文件原本是"用户自有的登录"。改为托管后，登录态由 Spark 复制/改写进隔离 home，退出即删除（现状 `defer os.RemoveAll(launchHome)` 已保证），真实 home 不被污染。

### 4.5 Management API（对齐 CPA `/v0/management/*`）

daemon 新增一组端点，供 CLI/TUI 触发登录：

```
GET  /v0/management/{provider}-auth-url     → { auth_url, user_code?, callback_port }
POST /v0/management/oauth-callback          → 提交 code，换 token 存 Store
GET  /v0/management/accounts                → 列出各 provider 登录态（账号、过期、来源）
POST /v0/management/{provider}-logout       → 删除 Auth 记录
```

TUI 登录面板（5.2）走这几条端点，与 CPA 的 `oauthTabModel` 对齐。

---

## 5. CLI / TUI 落地

### 5.1 CLI 命令（`internal/app/cli.go` 或新 `internal/app/auth_cmd.go`）

```
spark login  [--provider claude|codex|gemini|grok] [--device] [--no-browser]
spark logout [--provider <p>]
spark auth status          # 列出已登录账号
```

- 无浏览器时 `--no-browser` 打印 URL 由用户手动访问（对应 CPA `NoBrowser` / `LoginOptions`）。
- `--device` 强制 Device Flow（在无桌面环境 / SSH 时最有用）。

### 5.2 TUI 登录面板（`internal/tui/`）

新增 `auth_login_model.go`，复用现有面板结构（参考 `profile_manager_model.go` / `mcp_manager_model.go`）：

- 列表列出可登录 provider（Claude / Codex / Gemini / Grok）+ 当前登录状态。
- 选中 provider → 调 `GET /v0/management/{provider}-auth-url`，有浏览器则打开，device flow 显示 `user_code`（对齐 CPA `startOAuth`）。
- 回调完成后刷新状态，显示新登录的账号。
- 提供"登出"操作（`POST /v0/management/{provider}-logout`）。

### 5.3 daemon 暴露到非本机的鉴权

daemon 目前默认绑 `127.0.0.1`。若要让远程 client 连（CPA 场景），需在 `spark daemon run --addr 0.0.0.0:42337` 时开启下行鉴权（4.2 的 `config-api-key`）。本地回环保持免鉴权（现状 `spark-profile` 已够），避免破坏现有 TUI/CLI 的本地调用。

---

## 6. 与现有代码的改动点清单

| 文件 | 改动 |
|---|---|
| `internal/auth/auth.go`（新） | Auth 记录 + Store |
| `internal/auth/oauth/flow.go`（新） | OAuth PKCE 流程 |
| `internal/auth/oauth/device.go`（新） | Device Flow |
| `internal/auth/oauth/refresh.go`（新） | token 刷新 |
| `internal/auth/oauth/providers.go`（新） | 各 provider 端点参数 |
| `internal/config/config.go` | `Profile.AuthProvider` 字段；`EffectiveAPIKey` 兼容 |
| `internal/compat/engine/credential.go`（新） | `resolveRequestCredential`，登录态 vs API key 决策，供各 ingress handler 复用 |
| `internal/compat/ingress/{codex,claude,gemini}/handler.go` | 把 `EffectiveAPIKey()` 调用换成 `resolveRequestCredential(...)`；其余 Bifrost 调用（`ConfigureProvider`/`BindDirectKey`）不变 |
| `internal/compat/daemon/daemon.go` | `authenticateRequest` 链式 provider；Management API 路由 |
| `internal/compat/daemon/daemon.go`（路由） | `/v0/management/*` |
| `internal/integrations/claude.go` | 登录态注入 `ANTHROPIC_AUTH_TOKEN`；`shouldSkipClaudeMirror` 登录态分支 |
| `internal/integrations/codex.go` | 登录态注入 auth.json；`shouldSkipCodexMirror` 登录态分支 |
| `internal/integrations/grok.go` | 登录态分支链接 `auth.json` |
| `internal/integrations/gemini.go`（若有） | x-goog-api-key 注入 |
| `internal/app/auth_cmd.go`（新） | `spark login/logout/auth status` |
| `internal/tui/auth_login_model.go`（新） | 登录面板 |
| `internal/compat/daemon/daemon_test.go` 等 | 测试 |

---

## 7. 落地路线图（分阶段，可独立验收）

**阶段 A — 数据与登录骨架**（核心，最有辨识度）
1. `internal/auth`：Auth 记录 + Store（`~/.spark/auth/`）。
2. `internal/auth/oauth`：Device Flow + OAuth PKCE 流程（provider 参数先落 claude/codex）。
3. `internal/app/auth_cmd.go`：`spark login --provider claude --device` 能跑通并写 Auth 记录。
4. 单测 + 一次真实/仿真登录验证。

**阶段 B — 登录态注入与下行鉴权闭环**
5. `daemon.go`：`authenticateRequest` 链式 + `config-api-key` provider。
6. `engine/credential.go` + 各 ingress handler：`resolveRequestCredential`，把登录态以 `Bearer`/`x-api-key` 注入上游请求（Bifrost 调用不变）。
7. `claude.go`/`codex.go`/`grok.go`：官方 CLI 启动注入登录态（替换 `ANTHROPIC_AUTH_TOKEN` / auth.json / auth.json 链接）。

**阶段 C — TUI 与 Management API**
8. daemon `/v0/management/*` 端点。
9. TUI 登录面板 `auth_login_model.go`。

**阶段 D — 打磨**
10. token 刷新钩子、过期提示、登出清理、`--no-browser` 手动流程、错误态（无效/过期 token 的 401 提示）。

---

## 8. 风险与注意点

- **Claude / Codex 的 OAuth client_id / 端点**：不是公开稳定 API，需对照 CPA 仓库源码与上游行为，且可能随上游调整。阶段 A 用 Device Flow 起步可降低对浏览器 OAuth 端点细节的依赖（Device Flow 端点通常更稳定）。
- **登录态文件格式易变**：`~/.claude/.credentials.json`、`~/.codex/auth.json`、`~/.grok/auth.json` 是各家 CLI 私有格式。注入逻辑要对"解析失败则回退 BYOK"做降级，不让登录态破坏既有 key 流程。
- **token 刷新与并发**：daemon 是多客户端入口，登录态刷新需加锁（`internal/compat/daemon` 已有 `sync` 使用先例）。
- **安全**：Auth 记录含 token，Store 文件权限 `0o600`；daemon 暴露到非本机时下行鉴权必须开启。
- **兼容**：`spark login` 是新增能力，不改变现有 BYOK 行为；未配置登录态时一切照旧。