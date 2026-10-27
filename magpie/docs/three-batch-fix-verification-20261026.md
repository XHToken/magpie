# 端云协同 三批问题修复复核报告

日期：2026-10-26（北京时间）。

## 1. 复核结论

本次由三个子 agent 分别核查第一、第二、第三批报告，主 agent 交叉检查发现并统一执行隔离定向测试。

多数原始问题已有实质修复，但不能登记为“三批全部关闭”：

1. **P1：尝试次数上限漏修，已动态复现。**真实请求执行 17 次，账本只记录 16 次，最终成功用量被挂到第 16 条失败尝试。
2. **P2：自定义预算投影仍有边界不一致。**普通模式 Manager 配置 45 秒时 API 报告 45 秒，但整次脱敏准备仍受 30 秒规格/Pipeline 上限约束。
3. **P2：本地还原失败回云测试存在假覆盖。**该测试的混合组默认直接筛选云候选，未验证其注释声称的“先本地失败再回云”。
4. **文档问题：**第三批报告将非 Chat 本地流式拒绝描述成固定 HTTP 400，与允许回云后整体成功等实际路径不一致。

第二批两项核心程序缺陷已有修复并通过本轮目标测试。第三批协议检查、文档基线已有修复，但真实 Desktop 旅程、标题真实里程碑、多模型真实切换及最终交付版本仍缺完整证据。

本报告仅复核，不直接修改业务代码。模型生成质量和此前暂缓的 Mac/CUDA/GPU 实机事项不纳入本次结论。

## 2. 对象与本轮验证

- 工作区 `D:/api-router`，分支 `main`，HEAD 为 `916776d78d2acb70ec22e128b5a4bf74b0b41acb`。
- 当前改动主要仍在未提交工作区。不能只凭 HEAD 重建本轮候选。
- 没有启动真实宿主、模型、日常 Router 服务，没有修改用户配置或调用真实第三方上游。
- 测试使用隔离用户目录、假运行时、合成正文、回环模拟上游及临时 SQLite。
- 新增的审查测试及运行脚本均在忽略的 `magpie/dist/three-batch-verification-20261026/` 下，不修改生产源码或原有测试。

| 验证 | 本轮结果 | 范围与限制 |
| --- | --- | --- |
| 原生 Windows 定向 Go | 96 个测试及子测试通过，0 失败 | 第一/二/三批对应目标；不是全仓回归；本轮该步骤不带 race |
| WSL/Linux 定向 race | 6 个目标重复 3 次，36 个测试及子测试通过，0 失败 | 准入、本地还原、Pipeline 生命周期、观测过期清理；不等同于原生 Windows race |
| 标题插件 Node 测试 | 1/1 通过 | mock client；不是真实 Loomy UI |
| 新增 17 次派发边界复现 | 明确 FAIL | 原生 Windows、真实 handler＋模拟上游＋实际 SQLite，业务断言失败 |

原生定向测试记录的 16 个关键文件、race 记录的 4 个关键文件，在各自运行前后摘要一致。此摘要核对不代表整个仓库没有其他变动。

证据：[原生摘要](/D:/api-router/magpie/dist/three-batch-verification-20261026/focused-summary.json)、[原生日志](/D:/api-router/magpie/dist/three-batch-verification-20261026/focused-tests.jsonl)、[race 摘要](/D:/api-router/magpie/dist/three-batch-verification-20261026/race-summary.json)、[race 日志](/D:/api-router/magpie/dist/three-batch-verification-20261026/race-tests.jsonl)。

## 3. 仍需处理的发现

### V-01 / P1：将账本上限扩到 16，并没有限制真实派发或完整记录超限结果

**本轮动态复现。**建立九个顺序成员，前八个各两次返回 503，第九个首次成功。审查测试同时接受两种正确行为：真实限制最多 16 次；或者完整记录第 17 次。当前两者都没有做到。

```text
actual_dispatches=17
http_status=200
ledger_status=succeeded
stored_attempts=16
details=16
last_number=16
last_http_status=503
```

第 17 次的成功用量出现在第 16 条 HTTP 503 记录上。原有三次尝试新回归通过，未能覆盖该边界。

**原因。**[State.BeginCloudAttempt](/D:/api-router/magpie/internal/routerrequest/request.go:98)仍按每成员两次限制；没有全请求 16 次派发上限。[mergeGroupAttempts](/D:/api-router/magpie/internal/routerservice/group_routing.go:353)截断到 16，[观测总数](/D:/api-router/magpie/internal/routerobserve/observe.go:236)再次取 min；[最终用量](/D:/api-router/magpie/internal/routerobserve/observe.go:301)归入被截断列表的末条。

**应修。**要么在执行前统一限制全请求总派发次数并给出明确错误，要么让真实总次数、明细截断标志及最终成功尝试身份独立准确记录。不能只修改常量或删除一个 `min`。保留直连有限重试和请求总期限。

**关闭证据。**覆盖 16/17 次边界、多成员和 patience、取消/超时、最终目标及 usage、重启后查询。真实出站次数与账本表达一致，不再把成功用量归到失败尝试。

复现：[审查测试](/D:/api-router/magpie/dist/three-batch-verification-20261026/review_b1_attempt_cap_test.go)、[overlay 映射](/D:/api-router/magpie/dist/three-batch-verification-20261026/attempt-cap-overlay.json)、[失败日志](/D:/api-router/magpie/dist/three-batch-verification-20261026/attempt-cap-test.jsonl)。

### V-02 / P2：普通模式自定义 45 秒时，API 仍未报告整次准备的实际上限

默认 30 秒投影已修，日常默认路径不受此发现影响。受信任内部配置将 Manager 设为 45 秒时，[state()](/D:/api-router/magpie/internal/routerservice/runtime_redactor.go:255)直接取 Manager 的 per-call 预算返回 45000。

但正式 adapter 的 [Pipeline Budget](/D:/api-router/magpie/internal/routerservice/runtime_redactor.go:65)来自 [spec.Budget()](/D:/api-router/magpie/internal/routerredact/privacy_specification.go:178)，仍是 30 秒；[Pipeline.New](/D:/api-router/magpie/internal/routerredact/pipeline.go:137)也会把大于 30 秒的预算压回 30 秒。现有 [normal_custom_45s 测试](/D:/api-router/magpie/internal/routerservice/redaction_test.go:329)反而期待 45000，并在本轮通过。

这是当前源码和通过的状态测试共同确认的投影边界；本轮没有等待 30 秒测实际超时。应统一“单次模型调用预算”“整次脱敏准备预算”“某请求剩余期限”的含义。状态应报告对应有效上限，不以回显 Manager 字段代替实际执行语义。

**关闭证据。**普通/实验、默认/更短/大于全局上限的配置均与实际准备预算对齐；多批次不重置总期限；测试断言执行语义，不能仅固化旧显示值。

### V-03 / P2：回退测试没有触发所声称的本地失败

[TestGroupLocalCandidateRestoreFailureFailsClosedWithoutCommittingOutput](/D:/api-router/magpie/internal/routerservice/group_routing_test.go:1512)的多候选场景，创建了启用组级脱敏、同时含本地与云成员的组，却没有使本地目标优先可达。生产入口会在这种默认组合下筛选云候选，因此[回退断言](/D:/api-router/magpie/internal/routerservice/group_routing_test.go:1562)实际可由“直接云端成功”满足。

该发现属于测试覆盖问题，不代表本地还原的核心修复仍然失效。应让本地路径明确可达，注入真正的还原失败，并断言“本地执行→还原失败→尚未提交→云端执行”的顺序及次数。单节点坏 JSON 场景也应与明确的 Restore 错误区分，防止其他解析错误遮挡目标路径。

本地工具参数、规则指定本地、流式取消/还原失败、工厂创建与关闭交错仍可补接线专项。没有动态缺陷证据的边界保持“待验证”，不登记为已确认业务 Bug。

### V-04 / 文档：非 Chat 本地流式不是固定 HTTP 400

[第三批报告](/D:/api-router/magpie/docs/third-batch-host-protocol-report-20261026.md:155)写“400 local_protocol_stream_conversion_unavailable”。实际本地转换会快速拒绝该能力，外层可以按策略回云并成功；仅本地或不同错误映射路径也未承诺同一个 HTTP 400。

[现有保护测试](/D:/api-router/magpie/internal/routerservice/final_review_conversion_test.go:155)明确覆盖零本地推理、一次云调用并整体成功。说明应改为“本地快速拒绝，按路由策略回云或返回相应错误”，避免调用者依赖并不存在的固定状态码。

## 4. 第一批逐项状态

| 报告编号 | 本轮状态 | 说明 |
| --- | --- | --- |
| B1-01 原文观测及保留 | 保留期修复已验证；采集行为继续保留 | 新增后台/查询物理清理及正常关闭等待；无新写入的过期删除回归通过。默认明文正文/替换原值仍保存，没有独立采集开关。当前设计明确允许这一行为，应如实登记产品边界，不能写成“不再保存原文” |
| B1-02 Manager 准入 | 核心修复及目标回归通过 | 显式 Manager 归一到受检查路径；正式 Invoke、实验授权、模型/指纹/suite 绑定及消费时复查已接入。排队中 suite 撤销的独立端到端场景仍可补验 |
| B1-03 多尝试统计 | 部分修复，不通过关闭验收 | 三次场景通过；V-01 的 17 次边界动态失败 |
| B1-04 预算投影 | 默认路径通过；自定义边界未关闭 | 默认普通/实验 30 秒已通过；V-02 保留 |

## 5. 第二批逐项状态

| 报告编号 | 本轮状态 | 说明 |
| --- | --- | --- |
| B2-01 本地脱敏响应还原 | 核心修复及目标回归通过；回退覆盖需补 | `writeRestoredLocalResponse` 已接三协议非流式及 Chat SSE，私有捕获后还原成功才提交；pipe 结束后等待任务；V-03 的回退测试不足保留 |
| B2-02 Pipeline 生命周期 | 原始缺口修复及竞态目标验证通过 | 先竞争发布再初始化，落选关闭，ephemeral 释放，in-flight 引用计数，退役关闭及服务退出清理均已接入；现有生命周期/还原目标在 WSL race 下重复三次通过 |

此前修复的重叠子串歧义、Responses summary_index 隔离、规则匹配错误阻断，未发现源码回退。模型质量不在本轮复评范围。

## 6. 第三批逐项状态

| 报告编号 | 本轮状态 | 说明 |
| --- | --- | --- |
| B3-H01 版本/候选 | 版本文档已修；候选交付待收口 | Loomy 改为本机 0.9.39 / OpenCode 1.14.22；最终源码和 EXE 摘要未完成统一冻结 |
| B3-H02 两宿主真实旅程 | 待真实集成验收 | 当前报告明确真实 Desktop 启动、工具/UI 交互和正式部署仍待执行 |
| B3-H03 标题插件 | mock 测试通过；真实版本待验 | 本轮 mock 1/1；真实 1/4/8 轮、重启、插件升级及恢复尚不能用该结果替代 |
| B3-H04 多模型端口 | 代码/专项回归通过；真实闭环待验 | 本轮多模型同步、次模型冲突目标通过；真实 Desktop 主次模型切换及历史混合端口处理仍需独立记录 |
| B3-P01 协议说明 | 主设计已修；报告措辞待改 | 标准子集、宿主扩展、非 Chat 本地流式边界已说明；V-04 保留 |
| B3-P02 Loomy 检查 | 修复及本轮回归通过 | `$comment` 已补齐；Chat 成功、Responses/Anthropic 的 check/apply 拒绝、零上游、配置不变目标通过 |
| B3-P03 逐格证据 | 固定夹具通过；真实 Desktop 层未完成 | 本轮宿主字段原生保留/跨协议拒绝回归通过，不能据此认证所有真实供应商或长工具历史 |

日常 EXE 仍为 10 月 23 日文件，摘要 `a241502610723e46eb5ce72f36752f151f24ef61c23818e0f0ac310569cbb2ed`。本轮没有替换它，不能把源码测试通过描述为日常服务已经部署新修复。

## 7. 完整回归证据的准确边界

- 历史 `run-UXvqp7` 为 failed / complete:false，构建、race、vet、Chromium 通过，WebKit 未全通过。
- 后续 `run-70xmce` 的构建与两引擎页面通过，但它是 --only 诊断，race/vet 未运行，状态为 partial / complete:false。
- 不能把两个不同候选或不同阶段的结果拼接成最终完整门禁通过。本轮新增定向结果也不替代完整门禁。

本次结论是：保留已有有效修复，补修 V-01、V-02，修正 V-03 测试与 V-04 文档；第三批真实闭环和最终交付继续保持待验。Mac/CUDA/GPU 及模型质量不因此恢复为本轮任务。
