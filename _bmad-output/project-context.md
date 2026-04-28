---
project_name: 'sub2api'
user_name: 'Sue'
date: '2026-04-28'
sections_completed: ['technology_stack', 'language_rules', 'framework_rules', 'testing_rules', 'code_quality_rules', 'workflow_rules', 'critical_dont_miss_rules']
existing_patterns_found: 37
status: 'complete'
rule_count: 65
optimized_for_llm: true
---

# AI Agent 项目上下文

_本文件记录 AI Agent 在本项目中实现代码时必须遵守的关键规则与模式，重点关注容易被忽略的项目本地约束。_

---

## 技术栈与版本

- 后端以 `backend/go.mod` 为准：Go `1.26.2`，Gin `1.9.1`，Ent `0.14.5`，Wire `0.7.0`。
- 前端以 `frontend/pnpm-lock.yaml` 为准：Vue `3.5.26`，Vite `5.4.21`，TypeScript `5.6.3`，TailwindCSS `3.4.19`。
- 前端核心库：Pinia `2.3.1`，Vue Router `4.6.4`，Axios `1.15.0`，vue-i18n `9.14.5`，Vitest `2.1.9`。
- Docker Compose 默认依赖 PostgreSQL `18-alpine` 与 Redis `8-alpine`；README 中的 PostgreSQL 15+/Redis 7+ 是部署兼容说明。
- README 徽章中的 Go `1.25.7` 与 `go.mod` 不一致时，代码实现和构建要求优先遵循 `backend/go.mod`。
- 前端构建产物必须输出到 `backend/internal/web/dist`；需要嵌入 UI 的后端二进制使用 `go build -tags embed`。

## 关键实现规则

### 语言特定规则

- Go 代码必须保持 `gofmt` 格式；`backend/.golangci.yml` 启用 `errcheck`，新增错误返回不能被静默丢弃，类型断言错误也需要处理。
- Go 包导入路径使用 `github.com/Wei-Shaw/sub2api/...`；后端生成代码位于 `backend/ent` 和 `backend/cmd/server/wire_gen.go`，不要手改生成文件。
- 修改 Ent schema 后必须通过 `cd backend && go generate ./ent && go generate ./cmd/server` 重新生成 Ent 与 Wire。
- TypeScript 使用 `strict`、`isolatedModules`、`noUnusedLocals`、`noUnusedParameters`；未使用参数应以 `_` 前缀命名，未使用局部变量不要保留。
- 前端导入项目源码使用 `@/` 别名；类型只用于类型位置时优先使用 `import type`。
- 前端 API 请求必须走 `frontend/src/api/client.ts` 的 `apiClient`，因为它统一处理 token、`Accept-Language`、GET 请求 timezone、响应 `{ code, message, data }` 解包和 401 refresh。
- 前端调用 API 后通常拿到的是解包后的 `data`，不要在业务组件里重复假设返回值仍包含 `{ code, data }` 外壳。

### 框架特定规则

- 后端遵循 `handler -> service -> repository` 分层；`backend/.golangci.yml` 用 `depguard` 禁止 `handler` 和多数 `service` 直接依赖 repository、Gorm、Redis，新增代码必须沿用接口注入边界。
- 新增后端依赖必须加入对应层的 `ProviderSet`，例如 `internal/handler/wire.go`、`internal/service/wire.go` 或 `internal/repository/wire.go`，再运行 Wire 生成。
- Ent schema 是数据模型源头；不要直接修改 `backend/ent` 下生成的实体、query、mutation 文件。
- 后端长期运行的后台服务通常在 Provider 函数中 `Start()`，并必须在 `cmd/server/wire.go` 的 `provideCleanup` 中有对应 `Stop()`/关闭路径。
- 前端 Vue 组件使用 Vue 3 Composition API 与 `<script setup lang="ts">`，复用 UI 优先放在 `frontend/src/components/common` 或领域组件目录，不要在页面里复制通用控件逻辑。
- 前端路由必须在 `frontend/src/router/index.ts` 中懒加载组件，并通过 `meta.requiresAuth`、`meta.requiresAdmin`、`meta.requiresPayment`、`titleKey` 接入现有鉴权、支付开关和标题逻辑。
- 前端全局状态使用 Pinia；认证态走 `useAuthStore`，站点配置、toast、loading、版本信息走 `useAppStore`，不要在组件中绕过 store 直接散落全局状态。
- Vite 开发模式会通过 `injectPublicSettings` 注入 `window.__APP_CONFIG__` 以避免首屏闪烁；涉及公开站点设置时要兼容注入配置和 API 拉取两条路径。
- `vue-i18n` 被别名到 runtime 版本，并启用 `__INTLIFY_JIT_COMPILATION__` 以适配 CSP；新增文案应进入 i18n 字典并用 `titleKey`/翻译 key 引用。
- 样式使用 TailwindCSS，暗色模式依赖根元素 `dark` class；新增 UI 需要同时检查 light/dark 状态。

### 测试规则

- 后端普通测试使用 `cd backend && go test ./...`；分层命令已有 `make test-unit`、`make test-integration`、`make test-e2e-local`，需要按改动范围选择。
- Go 集成测试使用 `//go:build integration` 与 testcontainers；Docker 不可用时本地会跳过，CI 中 Docker 不可用应失败。
- Repository 集成测试已有 `IntegrationDBSuite`、`IntegrationRedisSuite`、`testEntTx`、`testRedis`；新增 DB/Redis 集成测试优先复用这些隔离工具。
- 不要在新测试中使用已废弃的 `testEntSQLTx`；需要事务隔离时用 `testEntTx`，需要真实 client 行为时用 `testEntClient`。
- HTTP handler 测试使用 `gin.SetMode(gin.TestMode)`、`httptest.NewRecorder()`、`gin.CreateTestContext()`，并优先断言响应体、状态码和关键副作用。
- 流式网关、failover、计费、幂等、限流等边界逻辑必须补回归测试；已有测试会验证 SSE 写入后不得继续拼接下一次 failover。
- 前端测试使用 Vitest + jsdom + Vue Test Utils，测试文件命名为 `*.spec.ts` 或 `*.test.ts`，放在 `__tests__` 或组件邻近目录。
- 前端测试环境在 `frontend/src/__tests__/setup.ts` 中 mock 了 `requestIdleCallback`、`IntersectionObserver`、`ResizeObserver`，组件测试不需要重复全局 mock。
- 前端覆盖率阈值为 statements/branches/functions/lines 全局 `80%`；新增复杂组件、store、API mapper 时应补单测。
- 涉及 i18n 的前端组件测试可 mock `vue-i18n` 的 `useI18n`，避免依赖完整翻译初始化。

### 代码质量与风格规则

- 后端目录职责清晰：`internal/handler` 只处理 HTTP/DTO/响应，`internal/service` 放业务流程，`internal/repository` 负责 Ent/SQL/Redis 持久化，`internal/pkg` 放可复用基础能力。
- 后端 lint 启用 `gosec` high severity/high confidence，但排除了若干常见规则；不要因为排除项存在就引入明显的密钥、路径遍历或 SSRF 风险。
- Go 命名遵循项目现状：文件多为 snake_case，类型/函数使用 Go 导出规则；常见缩写按 staticcheck initialisms 保持大写，如 API、HTTP、ID、URL、TLS。
- 前端组件文件使用 PascalCase `.vue`，组合式工具放 `composables`，API 模块按业务域放在 `frontend/src/api` 或 `frontend/src/api/admin`。
- 通用前端控件优先复用 `components/common` 的 `BaseDialog`、`DataTable`、`Pagination`、`Toast`、`Input`、`Select` 等，不要重复实现相同基础控件。
- 后台表格/筛选/弹窗类 UI 应使用已有领域组件模式，保持 Tailwind utility class 风格和暗色模式 class 配套。
- 前端 ESLint 允许 `any` 和部分 Vue 规则放宽，但 TypeScript 编译仍严格；新增代码不要用 `any` 掩盖可表达的领域类型。
- 注释应解释非显然约束、并发/缓存/安全原因；不要给简单赋值和直观分支写噪音注释。
- `README_CN.md` 当前显示疑似编码损坏；更新多语言 README 或文档前需确认文件编码，避免继续扩大乱码。

### 开发工作流规则

- CI 以 `backend/go.mod` 的 Go 版本运行，并显式校验 `go1.26.2`；本地或 Docker 构建文件若出现旧 Go 版本，优先同步到 `go.mod`/CI 版本。
- 前端 CI 使用 Node.js `20`、pnpm `9`、`pnpm install --frozen-lockfile`；更新依赖时必须提交同步后的 `frontend/pnpm-lock.yaml`。
- 前端质量检查入口是 `make test-frontend`（CI 调用）以及 `frontend` 内的 `pnpm run typecheck`、`pnpm run test:run`、`pnpm run build`。
- 后端质量检查入口是 `backend/Makefile`：`make test` 会执行 `go test ./...` 和 `golangci-lint run ./...`；窄改动可先跑相关 package 测试。
- 修改 Ent schema、Wire ProviderSet、依赖注入构造函数后，必须运行 `cd backend && make generate` 并检查生成文件。
- 发布流程先构建前端并把产物上传到 `backend/internal/web/dist/`，再由 GoReleaser 构建带 `-tags embed` 的后端 release。
- Docker 生产镜像使用根目录多阶段 `Dockerfile`，不是 `backend/Dockerfile`；`backend/Dockerfile` 仍显示旧 Go 基础镜像，除非专门维护，否则不要把它当作权威生产构建入口。
- Docker Compose 部署依赖 `.env`，`POSTGRES_PASSWORD` 必填；`JWT_SECRET` 和 `TOTP_ENCRYPTION_KEY` 留空会导致重启后会话或 2FA 失效。
- 默认时区是 `Asia/Shanghai`，影响数据库时间、使用统计“今日”边界、订阅过期和日志；涉及日期统计时必须显式考虑 timezone。
- 安全扫描包含 `govulncheck ./...` 和 `pnpm audit --prod --audit-level=high`，前端 audit 例外必须登记在 `.github/audit-exceptions.yml`。

### 关键防踩坑规则

- 后端 HTTP 响应必须使用 `internal/pkg/response` 的统一外壳：成功 `code: 0`，错误包含 `code/message/reason/metadata`；前端依赖这个格式自动解包。
- 不要绕过请求体大小限制和上游响应读取上限；配置中全局 body 默认 `256MB`，非流式上游响应默认 `8MB`，代理探测响应默认 `1MB`。
- URL 安全配置很敏感：`security.url_allowlist.enabled=false` 时默认仍应拒绝不安全 HTTP，只有显式 `allow_insecure_http=true` 才允许；生产不要随手放开私网、HTTP 或任意上游。
- 上游响应头过滤、CSP、`trusted_proxies`、Turnstile、billing circuit breaker 都是安全边界；修改默认值必须考虑部署风险和向后兼容。
- 网关流式响应一旦已向客户端写入 SSE 内容，failover 不得再拼接第二个上游流；只能写入合规 SSE error 结尾，避免流内容损坏。
- Sticky session 和多账号路由依赖带下划线的请求头；Nginx 反代需要 `underscores_in_headers on;`，相关改动不要把这些头过滤掉。
- 计费相关逻辑要 fail-closed：billing cache、余额扣减、usage record worker、幂等记录失败时不要默认放行付费请求。
- OAuth/token refresh 逻辑有多平台差异（Claude/OpenAI/Gemini/Antigravity/Sora）；不要把一个平台的 token provider、刷新策略或 quota 语义直接套到另一个平台。
- Sora 链路依赖独立配置和 `curl_cffi` sidecar，且 `use_openai_token_provider` 默认关闭；不要误走 OpenAI 普通刷新链路覆盖 linked Sora token。
- `RUN_MODE=simple` 会隐藏/跳过 SaaS、计费、分组、订阅等功能；新增后端校验和前端路由都要考虑 simple mode。
- 前端 backend mode 会阻断大多数非管理员页面；新增路由要检查 `backendModeEnabled`、公开路径白名单和 pending auth flow。
- 日志中必须脱敏 token、API key、OAuth secret、支付回调敏感字段；debug header 日志如 `gemini_debug_response_headers` 只能用于短期排障。
- 涉及日期范围、统计聚合、订阅过期、用量“今日”时，前后端都必须明确 timezone，不能用浏览器本地时间直接替代服务端边界。

---

## 使用指南

**给 AI Agent：**

- 实现任何代码前先读取本文件。
- 严格遵守所有规则；不确定时选择更保守、更受约束的实现。
- 新增模式或发现过时约束时，更新本文件。

**给维护者：**

- 保持本文件精简，只记录 Agent 容易忽略的项目本地规则。
- 技术栈、生成流程、安全边界或部署方式变化时同步更新。
- 定期移除已经显而易见或不再适用的规则。

最后更新：2026-04-28
