# 历史脱敏实施记录（2026-10-30）

本次修改已实现 Windows 上的可靠逐消息识别结果复用、完整请求已知值保护和显式分批补处理。它不是完整设计或发布验收完成声明。原先运行的 beta.13 和宿主端点未被替换。

## 已实现

- `cloudcodec.NewUserHistoryViewForApp` 提取所有实际用户消息；标题和观察页面继续使用当前输入专用视图。WorkBuddy/Loomy 每条消息分别剔除已知包装，工具结果不进入识别。
- `routerredact.ResultStore` 位于真实 `dirs.Layout` 的 `state/redaction-results`。当前用户 DPAPI 包装随机主密钥，核验 Windows 私有 DACL 和路径重解析点；HMAC 内容索引，AES-256-GCM 实体载荷，无原始整段历史、无普通 TTL。
- 正常请求先批量盘点历史。有效记录在重启、清空内存缓存、供应商或协议变化后继续复用；只允许明确位于末尾的新用户消息增量识别。未知旧消息返回 `redaction_history_prepare_required`，不自动推理或发送云端。没有可信编辑关联时，修改历史走整条显式补处理。
- 同内容识别合并等待；模型推理不占数据库事务或服务全局锁。完整识别提交成功后才允许生成出站快照。SQLite 使用 DELETE / EXTRA、有界 busy timeout、256 MiB 页限额；损坏、缺密钥、未知 schema、权限和提交失败保留现场并阻断。
- 从本次实际提供的用户原文结果汇总实体，在单请求随机命名空间中分配占位符。来源片段经过真实协议转换；助手、工具、普通 system/developer 文本只替换已知完整原值。控制字段、数字和未知扩展中残留已知原值时阻断。最终检查解码字符串、嵌套 JSON 参数和键，不能用字面角括号绕过。
- JSON/SSE 还原继续使用本次实际发出 token 的独立快照。另一个请求的 token、旧格式字面 token 和仅有会话 ID 均不能读取持久库原值。
- `POST /api/redaction/history` 提供 `create/status/step/cancel`，使用本机同源管理认证。创建不推理；每步最多一批、16 字段 / 2048 码点，最多选择 100 条 / 20 万码点。页面在批次之间暂停，显示进度，支持取消。已完成消息逐条提交；服务重启后重新提供原文，已完成记录不重扫。
- 账本与页面增加当前输入新识别/零命中/复用、历史复用、已知值替换、未对助手和工具新识别、完整出站检查字段。账本保留识别材料和最终正文证明摘要，实际云端尝试仍使用执行器真实计数。清空观察记录不删除可靠结果。
- 应用识别授权代次与云端供应商独立。删除应用、重建注册或改变凭据模式后旧域不能复用；发送尝试的准入与配置撤销在同一配置锁内确认。宿主连接开关本身不改变识别语义。

## 验证

新增 `TestHostHistoryKnownValueProtection` 在修改前分别对 WorkBuddy、Loomy 真实失败：第二轮最终云端正文泄漏历史电话号码。修改后通过，并断言第二轮识别器只看到新增的追问。

新增测试覆盖九种源/目标协议组合、重启恢复、内存清空、零命中持久化、显式历史准备、未完成结果阻断、整条历史编辑补处理、同内容并发合并、密文篡改、磁盘无原文、跨应用/授权代次隔离、控制字段和转义工具参数检查。HTTP 测试验证未知历史业务请求零模型/零云端调用，以及管理准备不调用云端。

现有使用固定 `<NAME:1>` 等 token 的集成模拟改为从实际请求中提取 token 后回显，以继续验证随机命名空间下的 JSON/SSE 还原。新增浏览器测试单独验证显式准备入口、选择、进度和取消；其中的管理响应是标明的页面模拟，后端行为由真实 Go HTTP 测试验证。

| 检查 | 实际结果 |
| --- | --- |
| 最终代码的 8 个受影响 Go 包 | 全部通过；包括 `cloudcodec/routerredact/routerconfig/routercloud/routerservice/routerledger/routerobserve/dirs`。日志：`D:/api-router/.git/history-head-checks/test.log` |
| 最终代码的 Windows 全库 vet、Linux nogui 全库 vet、产品构建 | 均通过。日志及结果：`D:/api-router/.git/history-head-checks/` |
| Windows 根程序构建、Linux nogui 根程序构建 | 通过。日志：`D:/api-router/.git/history-final-checks/` |
| 现有产品 Chromium / WebKit 套件 | 每个引擎 31 项全部通过，包含五语言和窄屏检查；该次 gate 未请求 race，整体状态明确为 partial。日志：`D:/api-router/.git/history-product-verification/` |
| 新增补处理页面测试 | Chromium / WebKit 均通过；各覆盖五语言、375/1600 宽度、选择、逐批进度与取消。管理响应为明确的页面模拟。日志：`D:/api-router/.git/history-final-ui/browsers.log` |
| WSL 脱敏包 race | `go test -race -count=1 ./internal/routerredact` 通过。最终复核日志：`D:/api-router/.git/history-final-race.log`；不包含 Windows 专属 DPAPI/ACL 测试 |
| 原生 Windows race | 无法执行：启用 CGO 后，环境缺少 `gcc`。不能以 WSL 结果替代 Windows 后端 race 验收 |
| `gofmt -l`、`git diff --check` | 通过；没有未格式化 Go 文件或空白错误 |
| `go test -tags nogui ./...` | **未通过**。日志：`D:/api-router/.git/history-final-checks/suite.log`；不宣称完整发布检查通过 |

全库运行中，`agent/cli/davsync/gateway/gui/library/provider` 包达到默认 10 分钟时限；`gateway.TestConcurrentCallerAttribution` 还出现设置文件读取/调用者归属失败。`pluginrt.TestPluginsImportOnlyCore` 扫到了工作区已有的 `dist/redaction-budget30-20261023/candidate/` 源码副本；`usage.TestDesktopSessionAccountMetadata` 的 Code tab 映射断言失败。这些包未在本任务中修改，未通过独立基线运行作进一步归因，也未掩盖为通过。这里列出实际失败而不把全库结果概括为绿灯。

## 尚未完成与发布限制

1. **可信会话生命周期和旧回显残留清理尚未实现。** 当前没有经过真实宿主验收的编辑/删除事件、持久 occurrence 绑定、revision/generation 事务、删除引用计数和稳定会话句柄。已编辑的未知历史会阻断并要求显式准备；源消息被移除而旧助手/工具回显仍在时，仍可能传出旧原值，该路径尚未实现设计要求的明确阻断。不能将这种宿主分支宣称为保护完成，需宿主重同步可核验原文；此项是发布阻碍。
2. **恢复管理未完成。** 尚无单条坏记录隔离/修复、可验证的部分初始化恢复、备份恢复和多 keyId 密钥轮换命令。当前保留现场并失败关闭，不支持自动删库重建；应保留整个目录，不以复制在线 SQLite 文件作为有效备份。
3. **补处理调度与进度持久化不完整。** 当前为显式手动单步，使用正常脱敏队列优先级；不保存未完成消息的分块进度。重启后已完成消息复用，未完成消息需从头补处理。尚未实现设计要求的低优先级调度及加密分步进度恢复。
   页面目前每次接收一条完整用户原文，尚未自动列出被阻断请求的缺失消息供逐条选择。多文本块消息应由宿主通过原协议管理 API 提交，手动拼接为一个文本块不能命中原多块消息的识别键。无可信关联时，一次带入多条未知用户消息也需要明确补处理，不能全部自动视为新增。
4. **beta.14 初稿的平台缺口。** 初稿的 Linux/macOS 缺少系统密钥与权限后端，生产脱敏明确阻断。此后已补充 Secret Service / Keychain 实现，当前代码和验证范围见[Unix 后端修复](redaction-unix-backends-20261030.md)；此前草稿安装包不自动包含后续源码。Windows DPAPI 文件不支持跨用户/跨设备恢复。
5. **完整验收仍有缺口。** 未完成真实 WorkBuddy/Loomy 编辑/删除事件能力实测、断电/文件系统故障恢复、引用删除竞争、全部崩溃点注入和一次性补处理成本压测。此记录不声明完整设计第 13 节全部通过，也不声明正式模型质量已获验收。

完整目标及验收矩阵仍以[设计方案](redaction-history-protection-plan-20261030.md)为准。

## beta.14 草稿复核（2026-10-30）

在提交源码与准备 beta.14 草稿时，找到了已有的 MinGW-w64 编译器并执行原生 Windows `-race -count=1`。`cloudcodec/routerredact/routerconfig/routercloud/routerledger/routerobserve/dirs` 七个包通过；`routerservice.TestHostOnlyCurrentPromptReachesModelAndRestores` 的宿主/目标协议子测试返回 `redaction_extraction_failed`，服务包未通过。单独 race 复核仍出现同样错误，未将其认定为偶发失败，也未修改生产代码来绕过。

完整日志保存在 `D:/api-router/.git/release-20261030/`。beta.14 只生成含候选包和校验文件的 GitHub Release 草稿，beta.13 继续作为公开版本。新测试结果不覆盖上文已列出的生命周期、平台后端和恢复工具发布阻碍；草稿不代表发布验收通过。其余候选检查及平台范围以草稿的 `BUILD-INFO.json` 和验证记录为准。
