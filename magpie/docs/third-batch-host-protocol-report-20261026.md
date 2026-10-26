# API-router 第三批问题报告：WorkBuddy / Loomy 闭环与协议支持矩阵

日期：2026-10-26（北京时间）。\
范围：仅 WorkBuddy / Loomy 最新候选闭环及协议支持矩阵。\
状态：源码、现有记录及本机安装元数据核查完成；已完成 B3-H01 文档基线对齐、B3-P01 云端与本地协议边界说明修正、B3-P02 显式 Loomy 检查口径对齐及定向自动化回归（真实 Desktop UI 闭环仍作为独立验收项保留）。

## 1. 范围与结论

按用户最新决定，本批只推进以下两个项目：

1. WorkBuddy / Loomy 与最新 API-router 候选的接入、请求、工具、流式、取消、重启及恢复闭环。
2. 两宿主与各上游协议组合的支持范围、检查口径、失败边界及说明的一致性。

Mac、CUDA、Metal、其他 GPU 实机兼容和 CPU/GPU 性能对照全部暂缓，不作为本批完成前置。模型识别/生成质量继续略过。第一、二批已登记的准入、统计、本地还原和缓存生命周期问题不在本文重复派发。

本批以集成验证和兼容边界收口为主。不能把未验证场景都写成已确认程序 Bug，也不能把旧版本或模拟测试的通过结果直接用于最新宿主。

| 编号 | 项目 | 当前问题 | 分类 |
| --- | --- | --- | --- |
| B3-H01 | 宿主闭环 | 文档版本基线已对齐（Loomy 0.9.39 / OpenCode 1.14.22）；候选与真实宿主运行证据待冻结 | 文档已对齐/运行证据待收口 |
| B3-H02 | 宿主闭环 | 最新候选缺两款 Desktop 的完整工具及恢复旅程证据 | 待集成验收 |
| B3-H03 | 宿主闭环 | Loomy 标题插件 v2 与真实版本的关联、作用域及恢复行为待验（端到端模拟闭环已验） | 待真實宿主集成验收 |
| B3-H04 | 宿主闭环 | WorkBuddy 多模型端口同步已修，真实切换及历史混合状态需分开验证 | 已修待验/历史状态处理 |
| B3-P01 | 协议矩阵 | 通用三协议子集支持、宿主原生扩展边界与本地非 Chat 流式限制已统一写入云端子系统文档 | 已修并验证 |
| B3-P02 | 协议矩阵 | 显式 Loomy 检查（`LoomyChecker`）已补齐 `$comment` 宿主特征并与 `/api/apps/check` 对齐 | 已修并验证 |
| B3-P03 | 协议矩阵 | 固定夹具、最新宿主实际请求及受限组合尚需统一证据登记 | 回放及负向验证待收口 |

## 2. 核查对象及“最新版本”的定义

当前 Router HEAD 为 `916776d78d2acb70ec22e128b5a4bf74b0b41acb`，工作区包含本轮修复与新增回归测试。只记录 HEAD 不足以复现候选。

本次只读核对 Windows 卸载登记、登记路径中 EXE 的 VersionInfo 及 `resources/app.asar` 内嵌元数据：

| 对象 | 当前读到的版本/来源 | 不能由此推出的结论 |
| --- | --- | --- |
| WorkBuddy | `D:/workBuddy/WorkBuddy.exe`，FileVersion `5.3.8`；卸载登记同为 5.3.8 | 不是对公开发布最新版的核查，也没有在本次启动验证 |
| Loomy | `D:/temp/Loomy/Loomy.exe`，FileVersion `0.9.39`；卸载登记同为 0.9.39；`resources/app.asar` 内嵌 OpenCode `1.14.22` | 已确认 `0.9.39` 实际内嵌 `OpenCode 1.14.22`，但本次未启动真实 Desktop |
| Loomy 标题设计基线 | 已更新为 `Loomy 0.9.39`（内嵌 `OpenCode 1.14.22`） | 已与本机 `D:/temp/Loomy` 实包对齐 |
| 日常 Router EXE | `D:/api-router/magpie/dist/api-router.exe`，最后写入时间为 2026-10-23 17:33:33 | 不能代表当前工作区新修复已经部署 |

本报告的“最新”指后续验收时明确冻结的 Router 候选及选定的宿主版本，不宣称已经确认市场最新版本。本次没有联网查询更新、升级客户端或启动宿主。

验证前需要登记：完整源码候选或提交、Router EXE SHA256、宿主 EXE 路径和版本、Agent/OpenCode 内核版本、安装包或关键组件摘要、配置根及证据目录。不同来源分别标为“源码调查”“安装元数据”“实际运行”，不得混用。

## 3. WorkBuddy / Loomy 闭环问题

### B3-H01：版本与证据基线对齐

**原问题与核查结论。**历史夹具及宿主实测固定 WorkBuddy 5.3.8、Loomy 0.9.39；原 Loomy 标题文档误写为 `0.9.40`。经提取本机 `D:/temp/Loomy/resources/app.asar`，确认本机 `Loomy 0.9.39` 实际内嵌的 OpenCode 版本即为 `1.14.22`（`embeddedOpencodeVersion: "1.14.22"`），且标题生成的第 1/4/8 轮触发条件与清洗逻辑均直接来自该 `0.9.39` 实包。[api-router-loomy.md](/D:/api-router/magpie/docs/subsystems/api-router-loomy.md:88) 已更新对齐为 `Loomy 0.9.39`（内嵌 `OpenCode 1.14.22`）。

**剩余收口项。**文档版本基线已统一；后续执行真实 Desktop 闭环验收时，仍需登记冻结的“Router 源码/EXE + 两宿主/内核 + 实机测试结果”证据包。

**关闭标准。**选择实际验收的版本，登记上述版本材料；核实标题 hook、配置文件结构及请求样例对应同一安装。使用现有安装可以形成明确版本的验收结论，无需在本批自动升级。若另有新安装，独立登记其来源，不借用旧版本结果。

证据：[Loomy 标题版本基线](/D:/api-router/magpie/docs/subsystems/api-router-loomy.md:88)、[宿主字段夹具来源](/D:/api-router/magpie/internal/routercloud/testdata/host-fields/README.md:1)、[历史交付范围](/D:/api-router/z-steps/integration-round2/delivery.md:39)。

### B3-H02：最新候选缺两款 Desktop 的完整业务闭环

**当前实现。**宿主文件、恢复记录及 Profile 已纳入原子连接/恢复事务；模型存在性、绑定和宿主状态也有校验。已有隔离 HTTP、文件事务、页面测试，以及部分历史真实宿主证据。

**剩余问题。**菜单出现、配置事务成功、短文本检查成功或内核 API 可用，均不能单独证明 Desktop 已加载新配置，或工具历史、流式停止及恢复符合预期。历史部分 fresh Desktop 工具复测曾被停止，不能记为失败，也不能补记为通过。

**未来最小验收场景。**每款宿主使用独立测试配置/项目及同一冻结 Router：

| 场景 | 应核对的行为 |
| --- | --- |
| 多模型及组接入 | 菜单与 Profile 的已绑定目标一致；至少切换两个目标，实际请求使用选中目标 |
| 多轮工具 | 用合成文件完成读、改、再读/命令执行的连续工具任务；工具调用 ID、结果 ID、参数和后续消息关联完整 |
| JSON 与 SSE | 宿主支持的实际请求模式得到正确内容、终止状态及工具结果；不能只测页面假数据 |
| 流式停止 | 已有内容后停止，Router 结束上游及本地处理，不再发起重复回答 |
| 停止后继续 | 同会话或新请求仍可正常完成，旧响应不混入新请求 |
| 重启宿主/Router | 加载已保存目标和实际地址，重复打开不产生另一套错误连接 |
| 断开及离线恢复 | Profile 与宿主状态同步；Router 停止后仍可按恢复记录执行离线恢复 |
| 冲突与失败 | Profile 保存失败、宿主文件被用户改动或恢复冲突时原子拒绝，保留可恢复信息，不覆盖用户新选择 |

这是程序和集成验收，不评价模型回答文采或智能水平。可以先用可控模拟上游验证工具和协议，再用选定宿主产生实际请求；模型随机输出不能作为程序断言的唯一依据。

**关闭标准。**两款 Desktop 分别有完整、同版本证据：宿主所选 target、请求 ID、实际供应商/模型、工具调用及结果关联、实际派发次数和终态一致；恢复保留其他供应商、用户设置及首次备份。

证据：[原子连接实现](/D:/api-router/magpie/internal/routerhost/connection_commit.go:15)、[自动接入提交](/D:/api-router/magpie/internal/routerservice/host_auto.go:137)、[宿主一致性校验](/D:/api-router/magpie/internal/routerservice/host_validation.go:76)。

### B3-H03：Loomy 标题插件 v2 的真实作用域及恢复待验

**问题性质。**这是新增集成验证项，当前没有在本轮确认标题插件故障。

插件依赖源会话关联文件、`ChatId`、会话到聊天映射以及 OpenCode hook 数据结构；它要取源会话实际选中的模型，并避免两套标题流程竞争。现有测试主要通过 mock client 和假上游覆盖。

需要在选定版本验证：

- 第 1、4、8 轮标题触发及更新；模型切换后标题跟随正确源会话。
- 手工标题不被覆盖；非 Router 会话不被重定向。
- 关联文件缺失、不可读或损坏时，不串到其他会话或供应商。
- 请求失败或空输出时保留原标题；用合成标记验证隐藏记忆及系统提醒的清洗范围。
- 重启后插件仍按既定版本生效；v1→v2 接入及离线移除保留其他插件和用户编辑冲突保护。

标题是否“写得好”属于模型质量，本批略过；标题调用了错误会话/模型、覆盖手工标题或恢复误删插件才登记为程序问题。

**关闭标准。**源会话、模型、触发轮次、手工保护、缺失关联及恢复路径均有选定真实版本证据；源码基线与运行版本一致。

证据：[标题所有权与输入契约](/D:/api-router/magpie/docs/subsystems/api-router-loomy.md:77)、[插件实现](/D:/api-router/magpie/internal/routerhost/loomy_title.mjs:16)、[插件测试](/D:/api-router/magpie/internal/routerhost/tests/loomy-title.test.mjs:8)、[服务标题测试](/D:/api-router/magpie/internal/routerservice/loomy_title_test.go:16)。

### B3-H04：WorkBuddy 多模型换端口已修，真实切换与旧混合状态需分开验收

旧问题为端口变化时只更新主模型，次模型仍指向旧地址。当前代码已经在事务内同步同一已验证旧 origin、Router placeholder 的全部受管模型，并有数组/对象、次模型冲突、备份及恢复专项测试，不能继续登记为未修。

待补的闭环为：在隔离配置中让服务合法换端口，分别从 Desktop 选择主、次模型并产生实际请求，确认都使用本实例实际地址；其他实例、真实 key 及用户模型保持原样。次模型发生冲突时应完整拒绝，并保留恢复信息。

旧版本已形成“主模型新端口、次模型更旧端口”的混合配置，需要结合真实配置根和恢复记录处理，不能假定本次同步会猜测所有历史地址。不能杀无关 listener，也不能把所有 endpoint 直接改成测试地址。

**关闭标准。**已修代码的多模型 Desktop 切换、重启及恢复通过；历史混合状态有明确、受恢复记录约束的处理结论。

证据：[10 月 26 日修复记录](/D:/api-router/magpie/docs/code-review-20261026.md:26)、[多模型同步测试](/D:/api-router/magpie/internal/routerhost/ports_multi_model_test.go:10)、[次模型冲突测试](/D:/api-router/magpie/internal/routerhost/ports_multi_model_test.go:67)。

## 4. 当前协议支持矩阵

两款宿主当前默认接入都发送 Chat Completions，请求入口分别为 `/app/workbuddy/v1/chat/completions`、`/app/loomy/v1/chat/completions`。Router 能接收三种协议，不代表这两款宿主实际从三种协议入站。

下表是当前实现/夹具边界，不是对每个真实云供应商或未来宿主版本的认证。

| 宿主与入站 | 上游协议 | 当前工程范围 | 真实宿主支持结论 |
| --- | --- | --- | --- |
| WorkBuddy / Chat | Chat | 文本、函数工具、工具历史、JSON/SSE；保留已验证宿主扩展字段 | 优先验收路径；绑定供应商及最新候选闭环待验 |
| WorkBuddy / Chat | Responses | 仅严格可移植子集；携带 agent、消息身份、toolResult、reasoning 或受限选项时拒绝 | 不能标默认完整兼容 |
| WorkBuddy / Chat | Anthropic | 同为受限转换；还需满足目标工具 schema、参数及历史语义 | 不能标默认完整兼容 |
| Loomy / Chat | Chat | 文本、函数工具、工具历史、JSON/SSE；保留已验证 SDK/内容元数据 | 优先验收路径；标题及真实宿主闭环待验 |
| Loomy / Chat | Responses | 仅严格可移植子集；`$comment`、reasoning、内容元数据或原生选项可触发拒绝 | 不能标默认完整兼容 |
| Loomy / Chat | Anthropic | 同为受限转换；目标协议不能表达的语义拒绝 | 不能标默认完整兼容 |

原生 Chat 中保留字段，表示 Router 不丢弃已验证的字段，不代表所有 Chat 上游供应商都接受或理解这些字段。原生 camelCase 控制项的意义也不由 Router 推断或改写。

### 按能力补充边界

| 能力 | 当前边界及本批要求 |
| --- | --- |
| 文本、多轮函数工具 | 通用可移植子集有三协议转换能力；需用选定宿主的实际序列回放，不能以单条文本探针代替 |
| WorkBuddy 宿主元数据 | `agent`、身份/追踪、`toolResult`、reasoning 等按已验证类型/角色保留；不能转换时在出站前明确拒绝 |
| Loomy 宿主元数据 | `$comment`、`reasoning_details`、`extra_fields`、refusal、`promptCacheKey`、`reasoningSummary` 等按各自契约处理；不能跨协议静默丢弃 |
| 工具 schema 和输出控制 | 目标不能等价表达的根 schema 组合、JSON 控制、并行工具约束或截断语义应保留或明确拒绝，不伪装成功 |
| SSE、取消、半途错误 | 支持路径保留工具参数分片及终止语义；已输出后失败不补发成功终止，也不重新派发完整回答 |
| token counting | 当前云端仅原生 Anthropic Messages→Anthropic 的计数操作；不推广为两宿主默认能力；本地计数未提供 |
| 媒体、远程历史、未知扩展 | 当前仍有明确拒绝边界；本批如实标注，不要求扩展为全部支持 |
| 本地执行 | 非流式可按受限契约转换；本地 SSE 当前仅 Chat；Responses/Anthropic 本地流式不支持。仅同步矩阵，不在本批扩展本地引擎 |

证据：[共享协议契约](/D:/api-router/magpie/docs/subsystems/api-router-codec.md:49)、[宿主字段原生/跨协议测试](/D:/api-router/magpie/internal/routercloud/host_fields_test.go:18)、[产品 HTTP 接线测试](/D:/api-router/magpie/internal/routerservice/host_field_integration_test.go:17)、[计数操作边界](/D:/api-router/magpie/internal/routercloud/count_tokens.go:13)。

## 5. 协议矩阵需要收口的问题

### B3-P01：统一宿主支持说明，纠正文档中的宽泛承诺（已修并验证）

**原问题。**原“支持三协议”表述容易被理解为两款宿主可以任意选择上游协议且保留全部能力，且原云端文档将本地执行也概括为三协议端到端保持，而实际代码对宿主专有扩展跨协议转换及本地非 Chat 流式均执行显式拒绝。

**修复与验证结果。**[api-router-cloud.md](/D:/api-router/magpie/docs/subsystems/api-router-cloud.md:72) 已更新，明确区分三项边界：
1. 标准子集（`chat`、`responses`、`anthropic` 的文本、函数工具调用、多轮工具结果、JSON 与流式事件）可在云端三协议间互转。
2. WorkBuddy / Loomy 宿主专有 Chat 扩展字段（如 `agent`、`$comment`、`reasoning_content`、`toolResult`、内容分片元数据等）仅在原生 `chat` 上游完整透传（`preserveChatExtensions`），跨协议目标（`responses` / `anthropic`）在出站前显式失败关闭（`400 capability_*`，零上游调用）。
3. 本地推理执行支持三协议非流式（`adaptLocalJSON`）及原生 `chat` SSE 流式，而 `responses` / `anthropic` 本地流式请求显式快速拒绝（`400 local_protocol_stream_conversion_unavailable`）。

多模型应用及混合协议路由组按每个实际成员校验能力；当主 `chat` 成员失败回退至 `responses` / `anthropic` 成员且请求含宿主专有扩展时，回退阶段同样遵循失败关闭（返回 `400 capability_*` 且不向上游发送有损请求）。

**关闭标准。**文档、供应商/应用检查提示和实际执行一致；限制不宣传为已支持；不会为了“通过矩阵”删除宿主字段或绕过校验。

证据：[云端文档协议边界](/D:/api-router/magpie/docs/subsystems/api-router-cloud.md:72)、[本地非 Chat 流式保护测试](/D:/api-router/magpie/internal/routerservice/final_review_conversion_test.go:155)。

### B3-P02：三种检查入口的范围不同，显式 Loomy 检查已对齐（已修并验证）

| 入口 | 当前实际检查 | 不应解释成 |
| --- | --- | --- |
| 自动接入 `/api/hosts/auto` | FastChecker 检查 Router 健康；外层验证目标、配置及事务一致性，不推理 | 已完成模型或 Desktop 工具兼容认证 |
| 应用检查 `/api/apps/check` | 经应用业务路径发短文本流式探针；WorkBuddy 加 `agent`，Loomy 加 `$comment`，跨协议不兼容可提前拒绝 | 已覆盖真实工具、多轮历史及所有扩展 |
| 显式 Loomy `/api/hosts/loomy` 的 `action=check` 与 `action=apply` | `LoomyChecker.Check` 已补齐 `"$comment": "connection-check"` 宿主特征，与 `/api/apps/check` 及 `ProductionWorkBuddyChecker` 口径一致 | 已覆盖真实工具、多轮历史及所有扩展 |

**原差异与修复结果。**修复前，`LoomyChecker.Check` 发送不含宿主特征的纯短文本 Chat 请求，导致绑定 `responses` / `anthropic` 供应商时 `/api/hosts/loomy`（`action=check` / `action=apply`）误判返回 `200 OK`（`verified:true`），而 `/api/apps/check` 与真实 Loomy 请求均因 `$comment` 返回拒绝。修复后，[`LoomyChecker.Check`](/D:/api-router/magpie/internal/routerhost/loomy_check.go:80) 已携带 `"$comment": "connection-check"`：当目标为 `responses` 或 `anthropic` 时，转换层在出站前返回 `400 capability_request_comment`（上游调用次数为 `0`），`/api/hosts/loomy` 的 `check` 与 `apply` 均返回 `409 Conflict` 且不改写宿主配置文件；原生 `chat` 目标正常检查继续返回 `200 OK`（`verified:true`）。

**关闭标准。**用原生 Chat 和受限跨协议两个对照，核对自动接入、应用检查、显式 Loomy 检查和真实请求的结果及说明；不兼容请求零上游调用。原生路径的正常检查继续成功，动态会话认证另列范围，不由 stored 模式检查替代。

证据：[FastChecker](/D:/api-router/magpie/internal/routerhost/check.go:27)、[外层目标校验](/D:/api-router/magpie/internal/routerservice/host_validation.go:18)、[应用探针特征](/D:/api-router/magpie/internal/routerservice/app_check.go:109)、[三协议探针对照](/D:/api-router/magpie/internal/routerservice/app_check_headers_test.go:121)、[Loomy 特征检查实现](/D:/api-router/magpie/internal/routerhost/loomy_check.go:80)、[Loomy 协议兼容回归测试](/D:/api-router/magpie/internal/routerservice/loomy_api_test.go:172)。

### B3-P03：固定夹具与选定真实版本请求需要逐格建立证据

当前宿主字段资料包含源码构造的单特征样例和少量脱敏后的真实 CLI 请求，不能全部称为真实 Desktop wire capture。供应商能否接收这些原生字段也没有由固定夹具认证。

本批后续验证应分两层：

1. **确定性协议层：**用模拟上游执行正确请求及明确拒绝的对照，记录字段、工具 ID、类型、事件顺序、终态及出站次数。覆盖直接绑定和路由组，不依赖真实模型生成质量。
2. **选定宿主层：**记录该 Desktop 实际产生的请求形状和版本，以一致替换方式脱敏；补缺少的多轮工具、恢复/压缩历史等形状，再通过相同 Router 入口回放。字段来自源码重建还是实际请求必须区分。

**关闭标准。**每个矩阵单元都有“支持且已验证”“明确受限/拒绝”“未验证”的结论和对应证据。受限组合正确拒绝即可，不按 HTTP 成功率强行放行；没有实际证据的单元保持未验证。

## 6. 执行与交付要求

建议先固定 B3-H01 的候选和版本，再处理 B3-P01/P02 的矩阵及检查口径，随后依据矩阵组织 B3-H02/H03/H04 与 B3-P03 的最小闭环。

每次记录应至少包括：Router 源码/补丁与 EXE 摘要、宿主及内核版本、配置根、协议组合、场景 ID、请求 ID、实际选中目标、模拟或真实上游类型、执行结果和未覆盖项。保留的请求/响应仅使用合成数据或经过一致脱敏的字段结构，不保存真实凭据和私有正文。

测试配置与日常配置分离。后续若启动普通项目，遵循根目录启动入口并使用其返回的实际管理 URL；不从历史测试记录挑 EXE/端口，不为迁就测试实例直接改宿主 endpoint。

当前已有宿主事务和协议自动化可以复用，无需重写相同测试。修复新增缺口后补针对性回归，并在最终完整候选上运行必要产品门禁。通用回归通过与真实宿主通过分别登记，不相互替代。

## 7. 本次完成与未执行项

- 已完成：
  - 重读当前设计、相关源码、现有夹具/测试和历史证据；核对登记路径里的两款 EXE 版本，并解包核实 `D:/temp/Loomy/resources/app.asar` 内嵌 `OpenCode 1.14.22`。
  - 对齐 [api-router-loomy.md](/D:/api-router/magpie/docs/subsystems/api-router-loomy.md:88) 的版本基线说明（`B3-H01`）。
  - 修正 [api-router-cloud.md](/D:/api-router/magpie/docs/subsystems/api-router-cloud.md:72) 的三协议标准子集转换、宿主专有扩展原生保留与跨协议失败关闭、本地非 Chat 流式拒绝边界说明（`B3-P01`）。
  - 修复 [`LoomyChecker.Check`](/D:/api-router/magpie/internal/routerhost/loomy_check.go:80) 补齐 `"$comment": "connection-check"` 宿主特征，使显式 `/api/hosts/loomy`（`action=check` / `action=apply`）与 `/api/apps/check` 及 `ProductionWorkBuddyChecker` 口径一致，并补充 Go 单元/服务层回归与候选二进制端到端验证（`B3-P02`）。
- 统一回归状态：`run-UXvqp7` 已运行完毕（Go 14/14 通过；Node/UI 34/36 通过），其中 `loomy-host.test.cjs` 与 `tutorial.test.cjs` 的 WebKit 异步等待与按钮禁用时序问题已在工作区修复并通过 Chromium + WebKit 双引擎复验（详见 `dist/regression/loomy-ui-review-20261026/validation-both.log` 与 `dist/regression/tutorial-ui-review-20261026/validation-both.log`）。
- 待后续独立执行：真实 WorkBuddy / Loomy Desktop 客户端启动与 UI 闭环交互、第三方真实云端在线推理、程序正式部署（更新 `dist/api-router.exe`）及宿主升级。
- 按用户决定暂缓：Mac、CUDA、Metal、其他 GPU 实机兼容和 CPU/GPU 性能测量；不等待这些环境来完成本报告或矩阵整理。

本报告所识别的检查口径差异（`B3-P02`）与文档基线/承诺偏差（`B3-H01`、`B3-P01`）已完成修复与自动化验证；剩余工作聚焦于冻结正式构建后的真实宿主闭环证据登记。
