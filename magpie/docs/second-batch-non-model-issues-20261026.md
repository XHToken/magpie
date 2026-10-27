# 端云协同 第二批程序问题梳理（略过模型质量）

日期：2026-10-26（北京时间）。

本次按照“模型质量问题略过”的要求，重新核查第二批涉及的脱敏、工具还原、预算、缓存及取消恢复代码。仅保留与模型识别/生成质量无关的程序问题，不开展模型效果调优，也不重复第一批问题报告。

## 1. 结论

当前保留两项程序修复：

| 编号 | 问题 | 建议优先级 | 当前证据与状态 |
| --- | --- | --- | --- |
| B2-01 | 组级脱敏作用于本地执行后，本地响应没有对应还原 | P1 | 当前可达调用链缺少还原接线；本次未新增运行复现 |
| B2-02 | 组级脱敏 Pipeline 的竞争创建、缓存和退役实例缺少完整关闭路径 | P2 | 当前源码生命周期缺口；资源累计规模和影响待确定性测试 |

另有三处程序问题已有新修复，应核对验收记录，不继续重复开发；复杂工具与取消恢复的历史失败主要受模型结果影响，不能据此认定产品程序有故障。

本次只读核查并生成本文，没有修改业务代码、运行模型、启动/停止服务或新增测试。工作区有其他并行修改，代码位置以本次读取为准，应按函数名重新定位。HEAD 为 `916776d78d2acb70ec22e128b5a4bf74b0b41acb`，不能仅凭该 HEAD 重建未提交候选。

## 2. B2-01：本地候选输入脱敏后，回答未接对应还原

### 2.1 触发条件

路由组启用 `LocalRedaction`，且最终选中本地执行候选。该分支当前可达：

- 只有本地成员的组：没有云成员可用于替换候选集合，因此本地候选被保留。
- 同时有云、本地成员的组：规则通过显式 `Use/Target` 选择本地时，跳过默认的云候选过滤。

普通混合组在没有显式规则目标、且存在云候选时，会优先筛出云候选。既有该类测试通过，不能证明上述本地分支已经覆盖。

### 2.2 程序问题与影响

`executeLocalCandidate` 调用脱敏 `Prepare`，将执行输入替换成 `prep.Body`；但本地模型响应直接交给 `writeLocalResponseObserved`，没有接入该 prepared 对象的 `Restore` 或 `RestoreSSE`。

即使识别器完全正确，本地模型合法复述输入中的占位符时，宿主也可能收到未还原的占位符；回答文本或工具参数因此不符合预期。该问题与姓名识别准确率无关，也不等同于已经证实的隐私外泄。

| 代码位置 | 当前行为 |
| --- | --- |
| [组级脱敏及候选过滤](/D:/api-router/magpie/internal/routerservice/group_routing.go:107) | 本地组、显式规则目标可进入本地脱敏分支 |
| [本地执行函数](/D:/api-router/magpie/internal/routerservice/group_routing.go:458) | 本地执行入口 |
| [准备脱敏输入](/D:/api-router/magpie/internal/routerservice/group_routing.go:513) | 取得带还原函数的 prepared 对象 |
| [替换执行输入](/D:/api-router/magpie/internal/routerservice/group_routing.go:535) | 将 `execBody` 改成脱敏正文 |
| [直接写出本地响应](/D:/api-router/magpie/internal/routerservice/group_routing.go:563) | 没有使用 prepared 的还原函数 |
| [现有组脱敏测试](/D:/api-router/magpie/internal/routerservice/group_routing_test.go:868) | 主要覆盖混合组选择云端后的还原，不覆盖本地回答 |

### 2.3 修复方向

先按产品语义统一处理：

- 如果执行本地“不脱敏”的既定边界，应让本地候选跳过输入脱敏，保留云端候选的脱敏流程。相关组设置及页面说明保持一致。
- 如果组设置明确要求本地也使用占位符输入，则必须补齐非流式、流式及工具参数的响应还原和错误传播，不能只替换输入。

不能通过降低识别要求、删除校验或换模型来关闭此问题。

### 2.4 不依赖真实模型的复现与验收

使用固定假识别器，将合成姓名替换成占位符；假本地执行器按输入回显占位符。这样模型生成结果完全可控。

| 场景 | 验收要求 |
| --- | --- |
| 仅本地组开启组级脱敏，Chat JSON | 本地执行输入和宿主输出符合最终确定的本地语义，不出现意外占位符 |
| 混合组规则显式指向本地 | 验证真正走本地，不能被默认云候选过滤掩盖 |
| Chat SSE，跨 chunk 回显占位符 | 保持流式输出，最终内容正确；取消能结束输入和还原任务 |
| 本地非流式工具调用 | 工具参数保持有效 JSON、关联 ID 和原有类型；占位符处理正确 |
| 还原失败 | 按已有响应提交规则失败，不伪造成功，也不在已提交后另发一次完整回答 |
| 普通云端组对照 | 现有云端脱敏、JSON/SSE 还原不退化 |

Responses/Anthropic 的本地流式执行本来就受当前支持范围限制，不把该能力扩展作为关闭此问题的前置。

**关闭条件：**确定本地输入语义，修正对应调用链；上述本地 JSON、Chat SSE 和规则分支有确定性回归，假模型响应正确时程序输出正确。

## 3. B2-02：组级脱敏 Pipeline 生命周期未完整回收

### 3.1 当前代码问题

每次 `routerredact.New` 都会启动一个缓存过期 goroutine 和 ticker，需要 `Pipeline.Close()` 才能终止。`Clear()` 只清缓存和更新 epoch，不停止后台任务。

组级 redactor 有三类生命周期缺口：

1. **并发首次创建：**`getGroupOrchestratorRedactor` 先创建 redactor 并执行 `current()`，随后才 `LoadOrStore`。并发创建同一 key 时，未被选中的实例已经启动 Pipeline，却没有被关闭。
2. **缓存与非缓存实例：**全局 `groupOrchestratorCache` 没有配套的服务关闭删除路径；非 cacheable 分支创建的实例也缺少明确关闭归属。
3. **模型/上下文变化：**旧 Pipeline 只执行 `Clear()` 并加入 `retired`，组级版本没有统一关闭方法。重复变化会积累旧 Pipeline 及定时任务。

普通 `runtimeRedactor` 有服务生命周期关闭方法；不能将该方法视为组级 redactor 已经被关闭。

| 代码位置 | 已确认内容 |
| --- | --- |
| [组级实例缓存](/D:/api-router/magpie/internal/routerservice/runtime_redactor.go:352) | 全局 `sync.Map` 保存组级对象 |
| [先初始化、后竞争发布](/D:/api-router/magpie/internal/routerservice/runtime_redactor.go:385) | 落选的新实例没有配套 `Close` |
| [旧实例清理与退役](/D:/api-router/magpie/internal/routerservice/runtime_redactor.go:448) | 只有 `Clear` 和追加 `retired` |
| [Pipeline 创建](/D:/api-router/magpie/internal/routerredact/pipeline.go:154) | 启动后台过期循环 |
| [Clear 与 Close 的差异](/D:/api-router/magpie/internal/routerredact/pipeline.go:374) | `Close` 才停止 goroutine；`Clear` 不停止 |
| [过期循环](/D:/api-router/magpie/internal/routerredact/pipeline.go:396) | ticker 持续运行直到收到停止信号 |
| [普通 adapter 的关闭](/D:/api-router/magpie/internal/routerservice/runtime_redactor.go:215) | 关闭普通 adapter 及其退役 Pipeline，未覆盖组级缓存 |

### 3.2 影响与边界

长期运行、反复切模型/上下文、并发首请求或同进程反复创建服务，会积累对象和后台任务。单个缓存的条数和 TTL 有界，不代表 Pipeline 实例数量也有界。

这是独立的程序资源管理问题；本次没有测量累计规模，也没有证据证明它是历史 4B 长文本超时的原因。

### 3.3 修复与验收要求

- 明确组级缓存由哪个服务实例拥有，在服务关闭时删除对应项并关闭资源，避免保留旧 store/Manager。
- 竞争创建时关闭未发布的实例，或避免在竞争完成前启动后台任务。
- 模型/上下文切换后，在不破坏在途请求的前提下回收退役 Pipeline；需要长期保留的统计可单独累计，不应依赖无限保存运行实例。
- 处理 `current()` 与服务关闭的并发，禁止关闭后重新创建实例。
- 用确定性同步点测试并发首次创建、重复切换、非缓存路径及服务关闭。核对实例终止信号，避免只依靠易受其他测试影响的 goroutine 总数。
- 验证正在还原的请求快照仍能完成，关闭之后没有继续运行的过期任务或旧 Manager 引用。

**关闭条件：**缓存发布竞争、配置切换、正常关闭及重复服务生命周期均有资源终止证据；在途快照不被错误破坏；对象和后台任务不随这些操作持续增长。

## 4. 已有新修复的程序项：核对验证，不重复开发

| 项目 | 当前实现与状态 |
| --- | --- |
| 唯一子串定位漏算重叠匹配 | [privacy_parse.go](/D:/api-router/magpie/internal/routerredact/privacy_parse.go:151) 已从首匹配后一字节查找下一次匹配，覆盖中文和非 BMP 歧义；[测试](/D:/api-router/magpie/internal/routerredact/privacy_ambiguity_test.go:8) 已加入 |
| Responses 多条推理摘要混用流式通道 | [stream.go](/D:/api-router/magpie/internal/routerredact/stream.go:365) 已按 `summary_index` 分通道并保留补发事件索引；[测试](/D:/api-router/magpie/internal/routerredact/stream_summary_test.go:13) 已加入 |
| 规则匹配超时被当作无命中 | 当前已传播固定安全错误并阻断；[确定性测试](/D:/api-router/magpie/internal/routerredact/detector_failure_test.go:10) 覆盖首次及后续匹配失败 |

[10 月 26 日实现检查报告](/D:/api-router/magpie/docs/code-review-20261026.md:23) 记录了这些修复及相关反向验证。本次没有重新执行测试，完整候选结果应以该实现任务的最终记录为准。

坐标恢复策略已版本化为 v6，严格生成已配置 JSON schema；这些旧状态不再列为当前待修。

## 5. 工具与取消恢复：保留验证事项，不认定程序故障

历史复杂工具回环在模型抽取阶段超时或坐标校验失败，后续还原没有被执行。生命周期诊断在第一次恢复的内容解析失败后直接返回，后续短 deadline 和第二次恢复也未执行。

- [工具回环入口](/D:/api-router/magpie/internal/routeracceptance/privacy_roundtrip.go:204) 的前置抽取失败，不能作为还原器有 Bug 的证据。
- [生命周期诊断](/D:/api-router/magpie/internal/routeracceptance/privacy_lifecycle.go:200) 将恢复内容解析作为后续检查前置，导致模型质量遮挡了运行时验收。
- 已有 [生命周期夹具测试](/D:/api-router/magpie/internal/routeracceptance/privacy_lifecycle_test.go:86)、[流式多工具及取消测试](/D:/api-router/magpie/internal/routerredact/stream_test.go:145) 等独立覆盖，不能描述为完全没有程序测试。

后续可将运行时恢复结果与内容识别结果分别记录，在安全条件满足时独立完成取消、短 deadline、槽位释放和同进程恢复检查。已有覆盖有效的场景无需重复编写相同测试；只补未覆盖的接线及证据。

此项属于诊断组织和验证事项，不作为新增产品 Bug，也不要求本轮先优化真实模型输出。

## 6. 按用户要求略过的内容

- 中文姓名漏检、误检、类别错误。
- 模型输出越界坐标、重复实体、拆分姓名、合法空实体中的语义漏检。
- 在输出正确性校验正常时，程序拒绝这些无效结果的行为。
- 现有记录中 4B CPU 单块推理约 27.5 秒、整次接近 30 秒的性能表现；记录不能证明程序队列或超时逻辑出错。
- 为改善模型质量而调整提示词、改抽取协议、补姓名规则、换模型及重新做质量评测。

30 秒总 deadline 当前已经跨排队和批次共享，未发现按每批续期的现存问题。后端变化的缓存身份也已通过包含 Runtime/ExecutionPlan 的模型指纹失效，相关疑点排除。

本批程序处理顺序为：先处理 B2-01，再处理 B2-02；已有修复核对最终验证记录；模型质量项不纳入这两项的修复完成条件。
