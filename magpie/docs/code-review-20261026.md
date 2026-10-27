# 端云协同 实现链路检查（2026-10-26）

本轮以 `cmd/api-router` 实际产品入口为范围，沿模块调用、状态变化、配置落盘、失败回退和响应还原检查，修复了 13 处产品代码问题，并纠正了回归测试的旧假设。工作区原有未提交修改全部保留；表格仅列本轮新增修复，不能把整个 Git diff 视为本轮工作。

检查以源码与隔离测试为证据，不表示所有上游 Magpie 功能、真实供应商、真实模型、宿主版本或操作系统均已通过验收。

## 实际链路与检查范围

| 链路 | 主要模块及数据流 | 本轮检查重点 |
| --- | --- | --- |
| 启动与服务控制 | `cmd/api-router` → `dirs` / `routerconfig` → `routercontrol` → `routerservice` 监听与身份 → `routerhost` 端口同步 | 锁与进程身份、实际管理地址、端口占用、宿主事务、重启目录保持；原生 Windows 隔离脚本验证 |
| 管理接口与应用 | `routerservice/http.go` 同源/回环/管理认证 → provider/app/group/runtime API → `routerconfig` 保存与凭据文件 | 保存校验能否在实际执行时成立；应用绑定、路由组禁用和嵌套开关 |
| 模型请求 | `routerrequest` 认证/预算/上下文 → `routerservice/models_exec.go` → 组调度/本地判断 → `routercloud` / `cloudcodec` → 响应提交 | 重试与回退边界、具体模型目标、协议与 ExtraBody 合约、流式提交后不重复派发 |
| 本地模型 | `routerdevice` → `routermanifest` → `routerdownload` → `routerprocess` → `routerruntime.Manager` / `Generate` | 推荐与制品校验、CPU/GPU 选择、安装依赖诊断、冷启动、能力指纹失效、实际执行身份 |
| 脱敏与还原 | 服务 redactor → privacy specification / `Orchestrator` → rule / local LLM detector → `Pipeline` 映射与缓存 → JSON/SSE 还原 | 扫描不完整时阻断、严格坐标恢复、Unicode、摘要通道、真实预算投影 |
| 请求与脱敏观测 | 调度/执行 → `routerobserve` → `routerledger` 请求账本及独立脱敏观察库 → 管理页面 | CPU/GPU 归因、同请求 upsert、容量与保留期；页面由统一双引擎回归覆盖 |
| 交付及共享基础包 | `routerdelivery`、`routeracceptance`、`steady`、`edit`、`filememo` 等 | 纳入统一构建、26 包 race/vet；未对上游未接入产品的全部包作逐行审计 |

## 修复结果

| # | 问题与影响 | 修复位置 / 行为 | 回归证据 |
| --- | --- | --- | --- |
| 1 | 规则正则首次或后续匹配超时被当作无命中，部分扫描仍可能向云端发送 | `routerredact/detector_rules.go`、`orchestrator.go`：内部传播固定 `redaction_extraction_failed`，HTTP 路由统一为脱敏不可用并阻断，不暴露包含原文的底层错误 | `TestPipelineRejectsIncompleteRuleScan` 两种匹配阶段在旧实现均实际 FAIL，修复后 PASS；使用合成超时规则 |
| 2 | Responses SSE 多个 reasoning summary 混用一个通道，第一段结束后后续段失败 | `routerredact/stream.go`：按 `summary_index` 隔离，补发 delta 保留摘要索引 | `TestSSEResponsesWholeReplyKeepsSeparateReasoningSummaries`、`TestSSEResponsesReasoningSummaryFlushKeepsIndex`；包含真实共享 codec 生成的序列 |
| 3 | 坐标恢复漏掉重叠子串，把多种可能误认唯一匹配 | `routerredact/privacy_parse.go`：第二次查找从首次匹配后一个字节开始；保留显式合法坐标的原行为 | `TestPrivacyRecoveryRejectsOverlappingExactMatches`：中文和非 BMP 样本旧实现实际 FAIL，修复后 PASS |
| 4 | WorkBuddy 多模型接入换端口只更新主模型，其他模型继续使用旧服务地址 | `routerhost/ports.go`：在事务内更新同一已验证旧 origin、Router placeholder 的全部模型，保留其他实例及真实 key | `TestWorkBuddyPortSynchronizationUpdatesEveryManagedModel`、`TestWorkBuddyPortSynchronizationPreflightsSecondaryModels`；数组/对象、冲突不写、备份和恢复 |
| 5 | 供应商允许保存执行器必然拒绝的请求头和 ExtraBody，保存成功后每次调用失败 | `routerconfig/provider_options.go` 提供共用限制，`providers_validate.go` 与 `routercloud/cloud.go` 使用同一规则 | `TestProviderRejectsOptionsThatCloudExecutionCannotDispatch`：30 组旧实现实际 FAIL；拒绝时配置/凭据字节不变 |
| 6 | 规则选中嵌套组时绕过 Off、Disabled，重新执行已关闭目标；父组关闭整个后代组也可被绕过 | `routerconfig/groups_validate.go` 新增活动展开，普通路由和规则共用；规则递归继承父组排除列表，保持完整目录展开的旧语义 | `TestGroupRulesRespectNestedActivation`、`TestActiveGroupExpansionHonorsEveryAncestor`；真实 HTTP 调用核对被调用模型，旧实现及漏修边界均实际 FAIL |
| 7 | GPU 首次冷加载后能力检查仍比较加载前 CPU 指纹，实际通过却报不可用 | `routerruntime/manager.go`：将检查绑定到实际取得的运行进程 | `TestAutomaticBackendColdCapabilityCheckBindsLoadedRuntime` |
| 8 | 同后端、同执行计划卸载再加载，能力证据被无条件清空 | `routerruntime/manager.go`：仅实际绑定变化时清空证据 | `TestAutomaticBackendReloadKeepsEvidenceForUnchangedBinding`；另测 GPU→CPU 回退仍必须使证据失效 |
| 9 | Select/RefreshInstallation 将依赖缺失等精确错误降为 ErrNotInstalled，页面从 missing 变 unknown | `routerruntime/backend.go`：保留 CPU 回退运行时的具体依赖错误 | `TestWindowsDependenciesSelectionAndRefreshPreserveFailureCodes`，覆盖 fixed/auto × select/refresh |
| 10 | 首次 GPU 推理观测仍记录为冷启动前 CPU，底层与业务统计都可能归因错误 | `routerruntime/inference.go`：取得执行槽时保存真实 binding，通过诊断快照传给 `routerservice/local_observation.go`；未取得槽则保留入口身份，不在执行结束后借用新模型身份 | `TestManagerObserverColdAutomaticBackendUsesActualBinding`、`TestBusinessLocalObservationBindsColdAutomaticBackend`、`TestRuntimeJudgeObservationBindsColdAutomaticBackend`、`TestLocalObservationUsesSlotBindingAfterModelChange` |
| 11 | 脱敏观察库容量满时，更新同一请求仍预留新行，误删另一请求 | `routerledger/redaction_observations.go`：同一事务中判断 request ID 是否已存在，upsert 更新不额外占槽 | `TestRedactionObservationUpdateAtCapacityRetainsOtherRequests`：旧实现总数从 2 错减为 1，修复后保持 2 |
| 12 | 管理 API 正式脱敏仍展示 15 秒预算，实际已使用 30 秒 | `routerservice/runtime_redactor.go`：正式与实验路径都读取 Manager 的实际预算 | 简单投影修正，由现有管理接口和完整产品回归验证 |
| 13 | WebKit 鼠标点击入口不会自动聚焦按钮，教程记录到错误焦点，关闭后无法返回实际入口 | `routerservice/assets/tutorial.js`：手动打开记录事件的实际触发按钮，自动打开沿用既有焦点逻辑 | `tutorial.test.cjs` 主动聚焦其他按钮并屏蔽鼠标隐式焦点；三个入口旧实现全部实际 FAIL，新实现保留鼠标和键盘恢复要求 |

第 7–10 项也已在原实现用新增测试实际复现失败。涉及运行时与观测的新增测试，在 Windows 原生和 WSL 均通过 `-race -count=20`；路由组与账本新增测试在 Windows 原生通过 `-race -count=20`。脱敏包整体 race、配置/云端/宿主受影响链路 race 均通过。

本轮同步更新 owning subsystem 文档。设备推荐文档按已有实现修正阈值描述，没有改变用户工作区中的推荐策略；脱敏文档明确独立观察库存储原文/替换项的现有行为。供应商文档纠正协议、字段、真实 API 和私有明文凭据的合约，应用/存储文档纠正遗漏 `enabled` 字段时为 `false` 的默认值。

## 最终验证

验证环境：原生 Windows x64，Go 1.26.8、Node 24.18.1、Playwright 1.55.1。以下检查均已通过，分次证据如下：

| 检查 | 结果 | 原始证据 |
| --- | --- | --- |
| 最终产品构建 | PASS | `dist/regression/run-70xmce/build.log`、`summary.json` 中 `binarySHA256` |
| 26 个产品/共享包 race | PASS，`-count=1 -p 1 -race` | `dist/regression/run-UXvqp7/race.log`，383.6 秒；两个辅助命令包无测试文件，仍参与编译检查 |
| 同 26 包 vet | PASS | `dist/regression/run-UXvqp7/vet.log` |
| 最终 Chromium 全套 | 12 套页面、23 项测试 PASS，0 FAIL、0 SKIP | `dist/regression/run-70xmce/chromium.log` |
| 最终 WebKit 全套 | 12 套页面、23 项测试 PASS，0 FAIL、0 SKIP | `dist/regression/run-70xmce/webkit.log` |

Go 检查完成后，后续源码修改仅为教程 JavaScript 和页面测试，已重建产品并重跑两个引擎的全部页面。`run-70xmce` 使用 `--only=chromium,webkit`，因此脚本原始摘要正确显示 `partial / complete:false`；其中的 race/vet 为本次未请求，已经通过的 Go 证据见 `run-UXvqp7`。本报告不把该诊断摘要改写为一次五阶段完整门禁通过。

另已通过：

- 回归入口自身 19 项离线测试。
- `scripts/tests/start-project.test.ps1`：正常目录、原生生命周期、端口不符时保留宿主文件。
- `scripts/tests/auto-port.test.ps1`：端口占用、WorkBuddy/Loomy 同步、备用端口稳定、崩溃恢复、凭据不变。
- `scripts/tests/restart-layout.test.ps1`：复制重启命令保持实例/配置目录，校验保存地址与实际地址一致，重复启动复用 PID，错误保存地址不会冒充匹配实例，残锁保留与恢复。
- 本轮 Go 文件 `gofmt`、工作区 `git diff --check`。

两次较早完整回归分别在独立复审补出父组排除后代组、业务观测另有旧绑定入口后，由本任务主动停止并标记 interrupted；其证据 `dist/regression/run-26mH22/`、`dist/regression/run-6dpS17/` 不作为完整通过结果。已经停止的本任务测试进程及其临时数据已清理，日志和中断锁记录保留。

`run-N7qdG3` 的构建、26 包 race、vet 全部通过；Chromium 18/19 项通过，旧页面结构断言失败，入口据此阻断 WebKit。修复的测试问题为：

- 重启布局脚本把合法自动换端口视为失败。新增自持 listener 的 `-OccupyRestartPort` 确定性用例，旧断言在同 instance、`running=true`、保存地址正确时仍实际 FAIL；修复后改核对身份、配置、实际地址及 `previousAddress`，普通和占用场景都通过。原始偶发失败未保留实际地址，不能断言当次端口竞争的具体来源；确定性用例证明该断言存在假阳性。
- 页面测试仍假定用量导航仅有两项，遗漏工作区已有的“脱敏记录”页签。改为核对三项明确的导航值，并实际点击脱敏记录、验证选中态、互斥面板、API 返回及空记录渲染，覆盖五语言与三种宽度。
- 页面测试在 Windows 用 `SIGTERM` 强制结束进程，却期待正常停机后的令牌清理。正常停机用例改调用隔离实例的原生 `service stop`，强制退出仍由独立恢复测试覆盖。

后续 `run-UXvqp7` 的 Chromium 19/19 项全过；WebKit 因 Loomy 的一项测试和教程的一个子测试失败（连带父测试计为 16/19）。Loomy 失败已通过受控延迟初始 GET 确定性复现：旧测试在 Profile 尚未加载时误判未选择，点击后实际取消模型，保存空列表。修复测试等待真实加载及保存结果；两引擎单文件均通过。证据为 `dist/regression/loomy-ui-review-20261026/reproduce-webkit.log`、`validation-both.log`。教程失败属于表中第 13 项真实产品缺陷，保留焦点恢复要求并修复源码。

`run-Gl4uPW` 是焦点修复前启动的 WebKit 诊断，在确认需修改产品 JavaScript 后主动停止，不计通过。

教程的独立旧实现失败与修复后双引擎通过日志保存在
`dist/regression/tutorial-ui-review-20261026/reproduce-webkit.log` 与
`validation-both.log`。最终 `run-70xmce` 全套包含这些新增入口用例；运行结束后临时数据和本次活动测试锁已正常清理。

## 验证边界

- 未启动或改写用户正在使用的服务、模型或宿主配置；测试服务、假凭据、SQLite 和宿主文件均使用隔离临时目录。未下载模型，未向真实供应商发送请求。
- Windows 完整门禁不能代替 macOS/Linux 原生完整验收；WSL 的局部 race 也不代表完整 Linux 产品验收。未重新运行上游 Magpie 全部历史功能的 `go test ./...`。
- 已经由旧版本形成“主模型新端口、次模型更旧端口”的历史混合状态，需要按宿主恢复事务检查；本轮没有自动放宽恢复规则来修改未知实例。
- 脱敏观察库的七天窗口在查询时过滤；物理过期清理由数据库打开或写入触发，没有后台定时清除器。
- 此为已覆盖链路的审查和回归结果，不作“项目已无任何 bug”的保证。
