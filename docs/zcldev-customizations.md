# zcldev 本地定制功能清单

本清单用于将上游 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的新版本 tag 合并到 `zcldev` 时，检查本地定制功能是否保留。以后升级提示词只需引用本文并指定目标版本，无须重新列出全部提交 SHA。

- 首次整理日期：2026-11-17。
- 初始核对基准：`zcldev` 的 `194516c655ff0a673ec1996f673078715cc1bed3`，已合并上游 `v0.2.21`；后续升级结果见下方升级验证记录。
- 本文覆盖下表七个提交，归为五项功能；其他本地差异仍需对照升级前分支检查。
- SHA 用于追溯来源。升级验收以升级前的有效行为、本文约定及后续修订为准；保留提交历史本身不能证明功能正常。

## 提交与功能对应

| 功能 | 来源提交 | 说明 |
|---|---|---|
| C01：Request ID 查询 | `75f793819a58eff510d3d1e2419dc0fe031516c3` | 使用记录查询、参数转发及错误表列显示 |
| C02：可配置关闭等待时间 | `0afb8e3b1a73b8888ab165ddd4199f0953a5e0f7` | `SHUTDOWN_TIMEOUT` |
| C03：账号批量更新与模型映射 | `0a6b226d2deb2ef74573ab77155b16bbffa8ce9e` | 追加、替换别名、保留白名单及逐账号结果 |
| C04：账号报错数量统计 | `9c6e71f3d6cc22cabcaa3931e5985d408c25bd7b` | 统计接口、堆叠图及筛选联动 |
| C05：Mihomo 检测与邮件 | `737447039a7a56edfd0ff6a1e730825d31541ff7` | 原节点与业务入口独立检测、出口展示、任务及邮件 |
| C05：配置与展示完善 | `6899dd1a873500dfdcbe926b6afd6ce2b0e01da1` | 支持任意节点配置、配置回填和相关校验 |
| C05：有效期与地理位置完善 | `194516c655ff0a673ec1996f673078715cc1bed3` | 有效期调整为 1 小时，地理位置取自原节点 |

`9c6e71f3d` 虽然标题为“升级sub2api 0.2.17”，实际主要新增 C04；完整上游版本合并在它之前的 merge 提交 `17591e49e` 中。上述七个提交不是连续的纯定制区间，单独摘取它们不会自动带入中间的上游升级和合并适配。

## 升级原则

1. 记录升级前 `zcldev` 的 HEAD，并建立备份引用；核对上游仓库和目标 tag 后，将该 tag merge 到现有 `zcldev`。
2. 对照升级前最终代码保留本地定制。发生冲突或上游接口重构时，在新结构中适配功能，同时保留上游有效修复。
3. 同一功能的后续修订优先于早期实现。尤其 C05 必须保留三次提交叠加后的行为，避免恢复硬编码节点或旧的 15 分钟有效期。
4. 上游已提供等效实现时，可以整合使用；需记录对应关系，并验证本地约定的行为。本文路径允许随重构更新。
5. 除文本冲突外，还需检查 API、统计口径、数据库迁移、依赖注入、生成代码、缓存与前端交互的兼容性。
6. 升级后逐项记录验证结果、跳过原因和待确认问题；新增或修改定制时，同步更新本文。

## C01：使用记录 Request ID 查询

### 必须保留

- 管理端使用记录的用量、错误请求两个 tab 支持 Request ID 筛选，输入去除首尾空格；排行榜不显示该筛选。
- 用量及错误查询全链路传递 `request_id`。错误查询的 `/admin/ops/errors` 和 `/admin/ops/request-errors` 均读取并转发此字段，保持按请求 ID 精确匹配的语义。
- 错误表提供独立 Request ID 列，由列设置控制。共享 Ops 错误表未指定可见列时，保持原默认布局；用量表的 Request ID 默认隐藏，可手动开启。
- 保持列设置及已有持久化行为。`request_id` 与上游另有的 `upstream_request_id` 应分别处理。
- C04 的账号报错图表使用同一个 Request ID 筛选，页面与统计接口保持联动。

### 关键位置与验证

- 前端：[`UsageFilters.vue`](../frontend/src/components/admin/usage/UsageFilters.vue)、[`UsageView.vue`](../frontend/src/views/admin/UsageView.vue)、[`OpsErrorLogTable.vue`](../frontend/src/views/admin/ops/components/OpsErrorLogTable.vue)、[`usage.ts`](../frontend/src/api/admin/usage.ts)、[`ops.ts`](../frontend/src/api/admin/ops.ts)。
- 后端：[`ops_handler.go`](../backend/internal/handler/admin/ops_handler.go)、[`ops_repo.go`](../backend/internal/repository/ops_repo.go)。原提交主要补入口及转发，查询基础在其父版本已存在。
- 自动测试：[`ops_error_handler_test.go`](../backend/internal/handler/admin/ops_error_handler_test.go) 的 `TestOpsErrorHandler_ForwardsRequestIDFilter`；[`UsageView.spec.ts`](../frontend/src/views/admin/__tests__/UsageView.spec.ts) 的筛选转发及列设置测试；[`OpsErrorLogTable.spec.ts`](../frontend/src/views/admin/ops/components/__tests__/OpsErrorLogTable.spec.ts) 的独立列测试。
- 人工验收：使用已知 Request ID 在用量、错误两个 tab 查询，检查请求参数和结果；开启、关闭对应列，刷新页面确认设置行为。

## C02：可配置关闭等待时间

### 必须保留

- 环境变量 `SHUTDOWN_TIMEOUT` 接受 Go duration 格式，例如 `300s`、`1m30s`，允许首尾空白。
- 未设置或仅空白时默认 **5 秒**；格式错误、零值和负值回退为 **5 秒**，非空非法值记录日志。
- 该时长真正用于 HTTP server `Shutdown` 的 context deadline，并输出实际等待时长。
- 部署时，容器或进程管理器的终止宽限期需与关闭等待时间配合；此变量本身只控制应用侧等待时间。

### 关键位置与验证

- 实现：[`backend/cmd/server/main.go`](../backend/cmd/server/main.go)。启动流程、信号处理和优雅关闭重构时需重新核对接入位置。
- 自动测试：[`main_test.go`](../backend/cmd/server/main_test.go) 的 `TestParseShutdownTimeout`，覆盖空值、空白、秒数、组合时长、非法字符串、零和负数。
- 人工验收：在隔离环境分别设置有效值、非法值并触发正常关闭，检查日志及实际等待行为。

## C03：账号批量更新与模型映射

### 必须保留

- handler、service、repository 及前端 API 全链路传递 `model_mapping_mode`，保持下表三种模式的区别。

| API 模式 | 行为 |
|---|---|
| `append` | 按账号补入原映射中不存在的源模型；已有同源映射优先，冲突不覆盖。前端模型映射编辑默认使用此模式。 |
| `replace_mappings` | 替换别名映射，逐账号保留 `source == target` 的模型白名单；空对象可以只清空别名。前端“替换”操作使用此模式并要求确认。 |
| `replace` | 兼容整个 `model_mapping` 替换行为，保持与“仅替换别名”的区别。 |

- 校验模式、映射对象及源/目标模型格式，保留通配符规则：别名的源模型允许末尾单个 `*`（含全匹配 `*`），目标模型及 identity 白名单使用精确模型名。`append` 拒绝空映射；前端不完整映射不能误触发清空操作。
- `replace_mappings` 拒绝传入白名单 identity 行。新别名与账号原白名单同名，或原白名单格式不规范时，在写入前拒绝并报告相关账号和模型。
- 对依赖默认映射且未显式设置映射的 Antigravity、Grok、Gemini Google One OAuth 账号，`append` 拒绝隐式替换其默认模型范围。
- 追加或仅替换别名时，仓库逐账号锁定、校验并更新 JSONB，保留 credentials 的其他键。纯追加且无新增时不修改记录、不产生 scheduler outbox；同批有其他字段更新时，仍须完成那些更新。
- 追加模式返回逐账号新增、相同、冲突及未变化统计；显式映射操作展示逐账号成功/失败结果，结果页保留到用户关闭。后续绑定组等步骤失败时，仍保留已完成的追加统计。
- 普通白名单编辑保留完整映射提交方式；白名单留空可通过空对象表达支持所有模型。
- “同步上游模型”使用基准账号获取模型：选中账号模式取首个选择账号，筛选模式取预览基准账号。同步写入批量草稿，再以追加方式保存；失败不改草稿，丢弃未保存同步内容前需确认。

### 关键位置与验证

- 前端：[`BulkEditAccountModal.vue`](../frontend/src/components/account/BulkEditAccountModal.vue)、[`ModelWhitelistSelector.vue`](../frontend/src/components/account/ModelWhitelistSelector.vue)、[`AccountsView.vue`](../frontend/src/views/admin/AccountsView.vue)、[`accounts.ts`](../frontend/src/api/admin/accounts.ts)。
- 后端：[`account_handler.go`](../backend/internal/handler/admin/account_handler.go)、[`admin_account.go`](../backend/internal/service/admin_account.go)、[`admin_service.go`](../backend/internal/service/admin_service.go)、[`account_service.go`](../backend/internal/service/account_service.go)、[`account_repo.go`](../backend/internal/repository/account_repo.go)。升级需同时检查输入结构、JSONB 更新和 scheduler outbox。
- 自动测试：[`account_handler_mixed_channel_test.go`](../backend/internal/handler/admin/account_handler_mixed_channel_test.go) 的 `TestBulkUpdatePassesModelMappingMode`；[`account_repo_model_mapping_test.go`](../backend/internal/repository/account_repo_model_mapping_test.go) 的追加、无变化、其他字段更新、默认映射保护及保白名单替换测试。
- service 测试需 `unit` tag：[`admin_service_bulk_update_test.go`](../backend/internal/service/admin_service_bulk_update_test.go) 的 `TestAdminServiceBulkUpdateAccountsModelMappingMode`、`TestAdminServiceBulkUpdateKeepsMappingStatsWhenGroupBindingFails`。
- 数据库集成测试需 `integration` tag：[`account_repo_integration_test.go`](../backend/internal/repository/account_repo_integration_test.go) 中 `TestAccountRepoSuite` 的 `TestBulkUpdate_AppendModelMappingPerAccount`、`TestBulkUpdate_ReplaceMappingsKeepsEachWhitelist`。
- 前端测试：[`BulkEditAccountModal.spec.ts`](../frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts)、[`AccountsView.bulkEdit.spec.ts`](../frontend/src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts)。
- 人工验收：选择白名单及别名各不相同的多个账号，分别验证追加、冲突、替换确认、清空别名、其他字段同时更新、同步草稿及逐账号结果。

## C04：账号报错数量统计

### 必须保留

- 管理端 `GET /api/v1/admin/ops/account-error-distribution` 按账号和用户侧错误类别返回数量，包含 `accounts[].account_id/account_name/total/categories`。
- 与错误列表共用筛选解析、SQL 条件和 `MapUserErrorCategory` 分类。统计满足条件的 `ops_error_logs` 记录数，即 `COUNT(*)`，排除没有账号 ID 的记录。
- 保持客户端可见错误口径，适配上游对该共享口径的后续修订；避免将已恢复成功的上游尝试计入用户报错。当前共享口径包含 `cyber_policy` 的流式 200 错误特例。
- 后端按总量降序、同量按账号 ID 升序返回。前端按类别数量合计排序，同量按显示名排序；空账号名回退为 `#账号ID`，未知类别归入 `other`。
- 使用记录页保留按账号、按错误类别堆叠的“账号报错数量”柱状图，支持加载态、空态、横向滚动、总数提示和中英文文案。
- 初次加载、应用或重置筛选、刷新时更新图表。请求使用 `view: 'all'`，联动日期、用户、API Key、账号、分组、Request ID、模型、错误阶段、错误类别和状态码。
- 日期转换保持与页面查询一致；用请求序号避免旧响应覆盖新筛选结果。加载失败时清空统计并结束当前加载态。
- 接口继续受管理员权限及 Ops 监控启用检查约束。

### 关键位置与验证

- 后端：[`ops_handler.go`](../backend/internal/handler/admin/ops_handler.go)、[`ops_repo.go`](../backend/internal/repository/ops_repo.go)、[`ops_models.go`](../backend/internal/service/ops_models.go)、[`ops_port.go`](../backend/internal/service/ops_port.go)、[`ops_service.go`](../backend/internal/service/ops_service.go)、[`admin.go`](../backend/internal/server/routes/admin.go)。升级时保持接口、实现及 mock 一致。
- 前端：[`ops.ts`](../frontend/src/api/admin/ops.ts)、[`AccountErrorDistributionChart.vue`](../frontend/src/components/charts/AccountErrorDistributionChart.vue)、[`UsageView.vue`](../frontend/src/views/admin/UsageView.vue)。
- 自动测试：[`ops_error_handler_test.go`](../backend/internal/handler/admin/ops_error_handler_test.go) 的 `TestOpsErrorHandler_ForwardsAccountErrorBreakdownFilters`；[`ops_repo_account_error_breakdown_test.go`](../backend/internal/repository/ops_repo_account_error_breakdown_test.go) 的 `TestGetAccountErrorBreakdownGroupsCategoriesByAccount`；[`AccountErrorDistributionChart.spec.ts`](../frontend/src/components/charts/__tests__/AccountErrorDistributionChart.spec.ts)；[`UsageView.spec.ts`](../frontend/src/views/admin/__tests__/UsageView.spec.ts) 的账号报错分布测试。
- 人工验收：在固定时间范围及筛选条件下核对图表与错误明细，覆盖无数据、已删除账号名称回退、快速连续筛选和接口失败场景。

## C05：Mihomo 原节点、业务入口检测与邮件通知

本项以三次来源提交叠加后的实现为准。部署配置、扩容示例及完整验收步骤见 [`mihomo-proxy-monitoring.md`](mihomo-proxy-monitoring.md)。

### 检测与展示

- 独立展示原节点状态、当前新连接出口、业务入口状态。原节点检测固定走原节点 SOCKS 端口；业务检测走账号实际使用的代理端口，允许 Mihomo fallback。
- 原节点故障但备用可用时，原节点仍显示故障，业务入口可用，`success` 仍为 false；告警以原节点状态为依据。
- 同轮读取本 Pod 的 Mihomo 控制器 `127.0.0.1:9090`。出口表示检测时的新连接首选节点；控制器不可达、组不匹配或切换中时保持相应未知/切换状态，不影响已独立完成的连通性判定。
- Mihomo 连通性探测使用独立连接执行 `HEAD https://cp.cloudflare.com/generate_204`，要求 204、不跟随重定向并正常校验 TLS。质量检测也走原节点，结果与三列状态独立。
- 健康结果有效期为 **1 小时**，前后端及中英文文案一致；三列按各自时间判断过期，并提供检测时间和执行实例。缓存拒绝旧配置或旧检测轮次覆盖新结果。
- 原节点连通性成功后，通过同一原节点端口获取 IP 和地理位置，与业务检测并行，超时不超过单次探测超时。查询失败、超时或部分字段缺失不改变健康和告警；新检测不沿用旧位置，也不用备用位置代替原节点。

### 配置与持久化

- 支持任意原节点名、检测端口和 fallback 组，无须为增加节点修改源码。保留中文及内部空格支持；名称与 Mihomo 配置精确对应。
- 启用时三个字段一起填写，去除首尾空白后名称和组名各不超过 100 个 UTF-8 字节；端口为 1～65535，且与业务端口不同，业务地址限 loopback。
- 检测地址固定为 `socks5h://127.0.0.1:<检测端口>`。编辑回填已有配置，部分更新保留未提交字段，切回普通代理并保存时清空三项配置。
- 账户继续绑定业务代理记录。Mihomo 负责 fallback，sub2api 负责检测、展示和通知；保持账户绑定和代理管理状态。
- 保留 [`251_proxy_test_tasks.sql`](../backend/migrations/251_proxy_test_tasks.sql) 的代理字段、任务及目标表，以及 Ent schema、生成代码、DTO、导入导出和 Wire 接线。与上游新迁移合并时，检查迁移编号及已有数据库的升级路径。
- sub2api 与 Mihomo 需共享 loopback 网络命名空间，当前部署为同一 Pod；实际监听器、fallback 组及 SMTP 配置按部署文档维护。保存通过后仍需验证实际组配置和监听器绑定。

### 定时任务、邮件与并发

- 保留任务增删改、五段 Cron、时区、启停、立即执行和发送测试邮件。每任务支持 1～20 个代理、1～10 个收件人，失败阈值 1～20，探测超时 3～30 秒；启用需配置 SMTP，并验证组合运行预算。
- 同一代理只能属于一个启用任务。原节点连续故障达到阈值后按收件人汇总发送；持续故障不重复通知已成功接收的人，失败收件人在后续节点仍故障的周期重试。
- 定时检测确认恢复后清除故障周期，下次故障重新告警。配置错误、内部取消等未知状态不作为节点故障发信。
- 只有定时执行更新正式告警周期。立即执行和行内手动检测只刷新结果，不累计或清除正式周期；测试邮件也不改变正式周期。
- 任务和通知进度持久化到 PostgreSQL。执行、任务编辑和代理配置修改共用会话级 advisory lock；多 Pod 时由拿锁实例检测其本地 sidecar，避免重复执行。
- 真实探测配置变更会重置故障周期；仅名称等展示信息修改不重置。缓存通过配置版本及采样 ID 保护整轮结果，质量检测写入保持已有三列采样。
- 每轮整体最多运行 5 分钟，网络、SMTP 及取消清理有时间上限，重启只补一轮到期任务。保留通知进度与配置版本保护；SMTP 接受后、进度持久化前退出仍可能导致下轮重复邮件。

### 关键位置与验证

- 后端实现：[`proxy_health.go`](../backend/internal/service/proxy_health.go)、[`admin_proxy.go`](../backend/internal/service/admin_proxy.go)、[`admin_proxy_monitoring.go`](../backend/internal/service/admin_proxy_monitoring.go)、[`proxy_test_task_service.go`](../backend/internal/service/proxy_test_task_service.go)、[`proxy_test_task_runner_service.go`](../backend/internal/service/proxy_test_task_runner_service.go)、[`proxy_test_task_email.go`](../backend/internal/service/proxy_test_task_email.go)、[`proxy_test_task_repo.go`](../backend/internal/repository/proxy_test_task_repo.go)。
- 前端实现：[`ProxiesView.vue`](../frontend/src/views/admin/ProxiesView.vue)、[`ProxyHealthCell.vue`](../frontend/src/components/admin/proxy/ProxyHealthCell.vue)、[`ProxyNodeTestConfig.vue`](../frontend/src/components/admin/proxy/ProxyNodeTestConfig.vue)、[`ProxyTestTasksPanel.vue`](../frontend/src/components/admin/proxy/ProxyTestTasksPanel.vue)。
- 常规 Go 测试：[`proxy_health_test.go`](../backend/internal/service/proxy_health_test.go)、[`proxy_test_task_service_test.go`](../backend/internal/service/proxy_test_task_service_test.go)、[`proxy_test_task_email_test.go`](../backend/internal/service/proxy_test_task_email_test.go)、[`proxy_test_task_repo_test.go`](../backend/internal/repository/proxy_test_task_repo_test.go)、[`proxy_health_cache_test.go`](../backend/internal/repository/proxy_health_cache_test.go)。重点覆盖原节点失败但备用可用、位置查询失败及并行、1 小时过期、配置版本与缓存原子写入、故障周期、收件人重试、手动操作隔离、并发和取消。
- `unit` tag 测试：[`admin_proxy_monitoring_test.go`](../backend/internal/service/admin_proxy_monitoring_test.go)、[`proxy_monitoring_postgres_test.go`](../backend/internal/repository/proxy_monitoring_postgres_test.go)。
- PostgreSQL 测试：[`proxy_test_task_postgres_test.go`](../backend/internal/repository/proxy_test_task_postgres_test.go) 使用 `PROXY_TASK_TEST_DSN`；代理元数据往返测试使用 `SCOPED_ADMIN_TEST_DSN`。两者均需独立测试库，未配置时会跳过。
- 前端测试：[`ProxyHealthCell.spec.ts`](../frontend/src/components/admin/proxy/__tests__/ProxyHealthCell.spec.ts)、[`ProxyNodeTestConfig.spec.ts`](../frontend/src/components/admin/proxy/__tests__/ProxyNodeTestConfig.spec.ts)、[`ProxyTestTasksPanel.spec.ts`](../frontend/src/components/admin/proxy/__tests__/ProxyTestTasksPanel.spec.ts)、[`ProxiesView.monitoring.spec.ts`](../frontend/src/views/admin/__tests__/ProxiesView.monitoring.spec.ts)。
- 人工验收：按部署文档在隔离环境验证原节点实际链路故障、备用接管、控制器异常、定时告警与恢复、失败收件人重试、自定义节点增改、重启和配置变更、原节点地理位置来源及 1 小时有效期。

## 升级验证记录

优先运行以上各项的相关测试，再按变更范围完成仓库 CI 检查。Go 的 `unit`、`integration` tag 分别对应 `backend/Makefile` 的 `test-unit`、`test-integration`；前端可使用 `pnpm --dir frontend exec vitest run <测试文件路径>` 指定上述测试，并执行 `lint:check` 和 `typecheck`。根目录 `make test-frontend-critical` 仅运行其列出的子集，不能代替全部本地定制回归。

下表每次升级填写一次，可保存在当次升级提交说明或升级记录中。数据库、Mihomo 或 SMTP 测试若缺少环境，应明确记录跳过，避免将静态检查或无冲突合并视为完整验收。

| 检查项 | 结果 / 证据 / 待确认问题 |
|---|---|
| 目标上游 tag、升级前 HEAD、备份引用 | 2026-11-17：上游 `v0.2.22` = `b0fa9f3e6eeab1968a53e1289b69daa4bb40ca2b`；升级前 HEAD = `194516c655ff0a673ec1996f673078715cc1bed3`；备份分支 `backup/zcldev-before-v0.2.22-20261117`；通过双亲 merge 合并。 |
| C01：查询参数、精确匹配及列设置 | 实现与升级前一致；核对输入 trim、两个错误入口及用量转发、SQL 精确匹配、独立 Request ID 列、默认隐藏及持久化。相关 handler/repository 回归通过；与 C04 共 24 项前端测试通过。 |
| C02：配置解析、默认值及关闭流程接入 | 实现与升级前一致；`TestParseShutdownTimeout` 及 server 测试通过；静态确认解析结果接入 `context.WithTimeout` 和 `Server.Shutdown`，非法非空值记录日志。真实在途请求下 SIGTERM 验收仍待隔离运行环境确认。 |
| C03：三种映射模式、白名单、结果页及同步草稿 | handler/service/repository/API 链路、逐账号锁及 JSONB/outbox、三种模式、默认映射保护和前端交互全部保留；前端 115 项通过，repository unit 及真实 PostgreSQL 两个指定集成子用例通过，0 skip。保留上游新增的直通账号模型过滤开关与 identity 白名单通配符校验；别名源模型的有效通配符规则不变。 |
| C04：统计口径、筛选联动及图表 | 实现与升级前一致；核对共用 SQL/筛选/分类、`COUNT(*)`、客户端可见错误口径、`cyber_policy` 特例、排序与显示回退、筛选联动、防旧响应及失败清空。相关 handler/repository 回归通过；前端结果见 C01。 |
| C05：配置、三列状态、位置、任务、邮件及持久化 | Mihomo 探测、配置、缓存、导入导出与任务核心实现保留；三列状态、原节点位置、1 小时有效期、手动操作隔离及收件人重试已核对并回归。前端 36 项通过；真实 PostgreSQL 4 项通过，0 skip，覆盖元数据往返、任务 CRUD/手动周期隔离、并发唯一性、编辑与执行串行及取消释放锁。真实 Mihomo/SMTP 验收见待确认项。 |
| 上游等效实现整合、其他本地差异及兼容性变化 | 五项定制无需由上游实现替代；同时保留上游有效改动。对比升级前相对 `v0.2.21` 的全部 107 个差异文件，除 Wire 清理参数、版本号及本文记录的检查修正外，定制补丁保留。Ent 285 个文件与相同参数隔离生成结果一致；Wire diff 为空；原有未提交清单及其 `.gitignore` 白名单一并纳入版本管理。 |
| 自动测试、人工验收、跳过项目及原因 | 前端定制回归 175 项、CI 关键回归 599 项通过，lint/typecheck/build 通过；后端 Linux/Windows 嵌入前端构建及版本显示通过。后端 unit 最终复验 61 个测试包通过、0 失败，lint 为 `0 issues`；详细覆盖及跳过原因见下方。数据库 integration 仅运行与本地映射相关的两个子用例，并未运行完整 integration 套件。 |

### v0.2.22 冲突及兼容性处理

- 唯一文本冲突为 `backend/cmd/server/wire_gen.go` 的 `provideCleanup` 实参：同时保留上游 `usageBillingLaneCombiner` 和本地 `proxyTestTaskRunnerService`；两者的创建、Provider 和停止逻辑均保留，Wire 生成结果一致。
- 上游新增两个 `251_group_*` 迁移，与本地 `251_proxy_test_tasks.sql` 前缀相同。迁移器按完整文件名和 checksum 管理，三者修改不同表；保留已部署的本地文件名及内容，不重编号。原版 PostgreSQL/Redis 集成 harness 已成功执行包含三者的全部迁移；已有库将按完整文件名补跑缺失项。
- 上游 tag 内 `VERSION` 仍为 `0.2.21`，本次将其修正为 `0.2.22`；源码版本解析脚本及嵌入前端构建后的 `server -version` 均返回 `0.2.22`。Go 版本要求随上游升级至 `1.27.4`。
- 全量检查发现原有本地代码的 lint 问题：显式忽略 `strings.Builder.WriteString` 永远为 nil 的错误、错误文本以小写开头、移除未使用的旧质量判断辅助函数；不改变探测和通知逻辑。代理列表搜索测试的预期同步为既有的 `unknown/unknown/not_applicable` 状态。
- 基准与上游共有的 Ollama stale callback 用例在 Windows 粗时钟下连续两次 `time.Now()+5s` 可能相同，使 fixture 没有产生它要验证的新代际；原用例重复 10 次均失败。将测试中的新 reset 设为已保存 reset 加 1 秒后，相关 15 个测试重复 10 次全部通过，生产限流逻辑保持不变。
- 另一个基准与上游共有的目录协议测试使用 `time.Now().UnixNano()` 生成场景地址，粗时钟下会复用缓存键；原用例重复 20 次有 15 次失败。改用递增场景编号并在测试结束清理缓存键后，相关 CommandCode/catalog 测试重复 20 次全部通过，生产目录缓存和协议路由逻辑保持不变。

### v0.2.22 验证范围与待确认项

- 前端定制回归包括 UsageView、OpsErrorLogTable、账号错误图表、BulkEditAccountModal、ModelWhitelistSelector、AccountsView.bulkEdit、ProxyHealthCell、ProxyNodeTestConfig、ProxyTestTasksPanel、ProxiesView.monitoring，共 10 个文件、175 项；另运行根 Makefile 指定的 31 个 CI 关键文件、599 项。
- 后端使用 Go 1.27.4。Windows 全量 unit 首轮的符号链接权限及缺少 `sh` 问题，已使用同版本 Go 交叉编译的 Linux 测试二进制在 WSL 补验：sysutil 全部通过；repository unit 包含子项共 1249 PASS、0 FAIL、0 SKIP，两个专用 DSN 均指向临时 PostgreSQL。测试容器及其数据卷已清理。
- 最终 Windows 全量 unit 复验的 61 个测试包通过，包含子项共 28317 PASS、0 FAIL、46 SKIP；另有 50 个包没有测试文件。明确排除上面已在 Linux 补验的四个环境限制测试，命令为 `go test -json -tags=unit -skip '^(TestCgroupMemoryLimit|TestPgDumperHoldsMigrationLockThroughReaderClose|TestPgDumperReleasesMigrationLockWhenProcessFails|TestPgDumperReportsUnlockFailureAndDiscardsConnection)$' ./...`。46 个既有条件跳过主要涉及外部凭据、独立数据库/Redis DSN 和平台权限；其中 C05 的 4 个数据库测试已单独真实执行并通过，其余被跳过的外部环境用例不在本次通过声明范围内。
- 最终 `golangci-lint v2.14.0 run --timeout=30m ./...` 为 `0 issues`；Wire diff、Ent 隔离重新生成比较和 `git diff --check` 均通过。最终生产代码已在 Linux/amd64（`CGO_ENABLED=0`）构建并执行 `-version`，显示 `0.2.22`；此前 Windows 嵌入前端构建及版本校验也通过。
- 实际控制器组名、原节点监听器与账户业务端口绑定、原节点断链后备用接管、真实 SMTP 告警/恢复/收件人重试、重启与多 Pod 行为，仍需按部署文档在隔离部署中验收。本次没有运行生产探测或投递真实通知。
- 管理页面的真实数据比对、浏览器刷新后的列配置、连续筛选与故障注入，以及在途请求下关闭等待时间和容器终止宽限期，仍需实机验收；本次证据为代码核对、组件回归和独立数据库测试。
- 未自动修改历史账号配置。若部署中存在 identity 白名单使用 `*` 的无效记录，应改用精确模型名或合法的别名源通配符。
- 本次本机验证日志与升级前未提交文件快照保存在 `.git/zcldev-upgrade-v0.2.22/`，不进入仓库历史；本文保存可随提交查阅的验收结论。

### v0.2.23 合并与定制核对（2026-11-19）

- 目标仓库：`https://github.com/Wei-Shaw/sub2api.git`。`v0.2.23` 为 annotated tag，tag 对象为 `ce19a640eab5c4b5ef1827c7c1fcfce49569411a`，解引用提交为 `5af9afb588c4b014b3df9db7d4073af1f8ae01b6`。
- 升级前 `zcldev` HEAD：`3bba29d4d161bee6ed1a97893f8ebe882e135f9d`，工作区干净；备份分支：`backup/zcldev-before-v0.2.23-20261119`。使用 `git merge --no-ff --no-commit v0.2.23` 合并，通过验证后创建双亲 merge 提交。
- 本次没有文本冲突，也未发现需调整定制实现的语义冲突。对照升级前相对 `v0.2.22` 的全部 **111 个差异文件**，自动合并没有改动其中任何文件；随后仅主动更新内嵌版本号及本文升级记录。代码、测试、部署相关配置及 `.gitignore` 白名单均保留。
- 上游 tag 的 `backend/cmd/server/VERSION` 仍为 `0.2.22`；本地合并后不处于该 tag 的精确 checkout，版本解析会回退到此文件，因此将其修正为 `0.2.23`。
- 五项定制均继续使用原有效实现，无需由上游等效实现替代；上游 GPT-6.2 Sol/Astra、Ultra Fast 定价、缓存 TTL 用量/计费修复及流式空闲切换等改动全部保留。共享 `useModelWhitelist` 只增加新模型预设，不改变本地批量映射规则。

| 定制项 | 核对与验证结果 |
|---|---|
| C01：Request ID | 输入 trim、用量及两个错误入口转发、精确匹配、独立列、默认可见性、列设置持久化及与 C04 联动均保留。相关 handler/repository 测试通过；与 C04 共 3 个前端文件、24 项通过。 |
| C02：关闭等待时间 | 代码与升级前一致；`TestParseShutdownTimeout` 通过，覆盖默认值、空白、合法时长、非法值、零与负数。核对有效结果接入 `context.WithTimeout` 和 `Server.Shutdown`，非法非空值记录日志。 |
| C03：批量映射 | 三种模式、源通配符与 identity 校验、默认映射保护、逐账号 JSONB 锁和 outbox、保留各自白名单、无变化及其他字段更新、逐账号统计和失败结果、同步草稿与替换确认均保留。handler/service 测试通过；前端定制 3 文件、115 项通过，共享 composable 另 26 项通过；真实 PostgreSQL 两个指定集成子用例通过，0 skip。 |
| C04：账号报错统计 | API/权限、共享筛选与 SQL、`COUNT(*)`、排除空账号、客户端可见错误及 `cyber_policy` 特例、分类排序和名称回退均保留。页面日期/筛选联动、请求序号保护、失败清空、加载及空态已核对；handler/repository 与前端测试通过。 |
| C05：Mihomo 与邮件 | 任意节点配置、三列状态、原节点 IP/位置、1 小时有效期、缓存版本、独立探测、任务预算、手动周期隔离、告警及收件人重试、会话锁、导入导出和绑定关系均保留。前端 4 文件、36 项通过；真实 PostgreSQL 4 项通过，0 skip，覆盖元数据往返、迁移幂等、任务 CRUD、并发唯一性、编辑/执行串行、取消释放锁及手动周期隔离。 |

### v0.2.23 迁移、生成代码与验证范围

- 本地 `251_proxy_test_tasks.sql` 的文件名及内容保持不变；上游新增 `252_channel_pricing_ultrafast_multiplier.sql` 只修改渠道价格表，与本地迁移无冲突。原版 repository integration harness 在临时 PostgreSQL 18.1 / Redis 8.4 中成功执行全部 **306 个 SQL 迁移**。额外通过上游两个 Ultra Fast 定价集成测试，验证迁移可重复执行、保留原配置及新倍率 CRUD/非法值回滚；连同 C03 共 4 个目标用例，0 fail、0 skip。
- Ent schema/生成代码、Wire 声明/生成代码及依赖文件相对升级前均无变化，无须重新生成；保留上一轮已验证一致的本地接线与停止逻辑。
- 前端定制回归共 **175 项通过**；根 Makefile 指定的 31 个 CI 关键文件 **599 项通过**，额外上游变更的 3 个文件 **50 项通过**。`lint:check`、`typecheck`、生产 `build` 均通过。C03 共享 composable 的 26 项已包含于 599 项，不重复计数。
- Linux 完整 repository unit：711 个顶层测试、含子项 **1249 PASS，0 FAIL，0 SKIP**，包括 C05 的 4 项真实 PostgreSQL 测试及 Windows 缺少 `sh` 的 PgDumper 用例。Linux sysutil 的 3 个顶层及 9 个子用例全部通过，补验 Windows 符号链接权限受限场景。
- Windows 全量 unit 的 **61 个测试包通过**，含子项 **29678 PASS，0 FAIL，46 SKIP**；另 50 个包无测试文件。使用 Go 1.27.4，命令为 `go test -json -tags=unit -skip '^(TestCgroupMemoryLimit|TestPgDumperHoldsMigrationLockThroughReaderClose|TestPgDumperReleasesMigrationLockWhenProcessFails|TestPgDumperReportsUnlockFailureAndDiscardsConnection)$' ./...`，显式排除的四项已由上述 Linux 测试补验。46 项既有条件跳过涉及独立数据库/Redis、外部凭据、平台权限、仅生产历史数据或本地插件包；其中 repository 的 20 项已在 Linux 完整 unit 中真实执行并通过，其余条件跳过不在通过声明范围内，详细原因见本次 `backend-unit-summary.json`。
- Windows/amd64 与 Linux/amd64（`CGO_ENABLED=0`）均通过 `go build -tags embed -trimpath`，包含本次前端生产构建；两平台实际执行 `server -version` 均显示 **0.2.23**，源码版本解析脚本也返回 `0.2.23`。
- 与 CI 同版本的 `golangci-lint v2.14.0`（Go 1.27.4）检查为 **0 issues**。部署安全、Gateway 环境变量、运行资源、Caddy 缓存及 simple mode 五个 shell 检查通过；检查使用隔离副本将文本转换为 LF，避免 Windows `.gitattributes` 导出的 CRLF 影响逐行匹配，未修改实际部署文件。
- 数据库测试容器及其匿名卷均已清理，原有容器保留。未运行完整 `-tags=integration ./...`；本次真实数据库验证范围仅为上述指定用例及完整 repository unit。

### v0.2.23 待实机确认项

- 真实 Mihomo 监听器、控制器组名及账户业务端口绑定、原节点断链后 fallback 接管、真实 SMTP 告警/恢复/失败收件人重试、重启和多 Pod 行为仍需隔离部署验收；本次没有运行生产探测或投递真实通知。
- 管理页面真实数据与错误明细比对、浏览器刷新后的列设置、快速连续筛选与接口故障，以及真实多账号模型映射操作仍需实机验收。
- 在途请求下的 SIGTERM 关闭行为、`SHUTDOWN_TIMEOUT` 与容器终止宽限期仍需运行环境确认；未自动修改历史账号或部署配置。
- 本次验证日志、差异文件 blob 核对、隔离数据库说明及执行脚本保存在 `.git/zcldev-upgrade-v0.2.23/`；本文记录可随提交查阅的结果。

## 以后升级可复用的提示词

> 将上游仓库 https://github.com/Wei-Shaw/sub2api 的 `<目标 tag，例如 v0.2.22>` 通过 merge 合并到本地 `zcldev` 分支。
>
> 升级前记录 HEAD 并建立备份引用。读取 `docs/zcldev-customizations.md`，以升级前 `zcldev` 的最终代码为基准，保留全部有效的本地定制功能及相关配置，逐项处理冲突并验证。
>
> 上游已有等效实现的功能可以整合使用，需保持本清单约定的行为。检查清单之外的本地差异，并同步更新发生变化的实现位置、测试入口及兼容性说明。
>
> 完成后说明目标版本、冲突处理、各项定制功能验证结果，以及跳过项目和待确认问题。

## 后续维护

- 新增独立功能时增加功能编号、行为要求、关键位置、验证方式及来源提交；同一功能的后续修复合并到原条目。
- 功能调整时同步修订行为要求，避免旧提交描述与当前实现冲突；路径或测试重命名时更新链接。
- 上游吸收、替代或业务确认移除某项定制后，在对应条目记录版本、原因及验证结果，保留追溯信息。
- 本清单随仓库提交维护。以后升级请求引用清单和目标版本即可，提交 SHA 只需在新增或修订条目时登记。
