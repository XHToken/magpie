# Loomy 模型显示名称修复

日期：2026-10-26。

## 根因与本机证据

Router 原先在 `provider.api_router.models` 中把同一个值同时用作模型键和
`name`，例如 `p_afb…/deepseek-chat`。模型键用于请求路由，`name` 用于菜单展示，
两者不应共用内部编号。

只读检查本机 Loomy 配置确认：截图中的两个 DeepSeek 模型、本地模型和
`group/2222` 的 `name` 均等于完整路由 ID，且没有 `displayName` 覆盖。
Loomy 源码 `src/lib/chat-model-selection.js` 的 `deriveSelectableModels` 依次读取
`displayName`、`name`、模型键；`chat-model-selector.jsx` 展示 `modelName`。
因此修复位置在 Router 的宿主配置生成链路。

## 方案与实现

| 类型 | 请求中的模型 ID | Loomy 显示名称 |
| --- | --- | --- |
| 云端 | `<providerId>/deepseek-chat` | `deepseek-chat · DeepSeek` |
| 云端 | `<providerId>/deepseek-reasoner` | `deepseek-reasoner · DeepSeek` |
| 路由组 | `group/<id>` | 用户配置的组名 |
| 本地 | `local/spark-x2.5-1.7b` | `Spark-X2.5-1.7B` |

[`hostModelNames`](../internal/routerservice/host_model_names.go) 从服务端保存的供应商、
有效路由组和本地模型目录生成名称；云端模型名放在供应商名前面，避免长供应商名遮住模型。
供应商/目录元数据缺失时回退为模型部分。名称不作为请求 ID，也不接受客户端直接指定。

[`Options.ModelNames`](../internal/routerhost/types.go) 将显示元数据传给 Loomy 适配器。
普通自动接入和独立目录的首次预览/应用均使用同一个解析器。

旧连接在 `/api/hosts/auto` 重新连接时升级：

- 更新旧的完整 ID 名称，以及供应商/路由组改名后的自动名称。
- 对照原始备份和上次生成的配置辨认用户别名；重新排序、切换主模型和新增模型时也保留别名。
- 在同一个宿主/Profile 事务中更新名称和恢复记录，保存失败时两者一起回滚。
- 保留首次接入前的恢复基线；只读状态接口不写配置。

事务实现见 [`synchronizeLoomyModelNames`](../internal/routerhost/loomy_names.go)，
完整契约见 [Loomy 子系统说明](subsystems/wefto-loomy.md)。

## 验证结果

| 验证 | 结果 |
| --- | --- |
| 使用原 `host_auto.go` 的 Go overlay 运行新增集成用例 | 真实 FAIL：显示名称仍为 `p_…/deepseek-chat`、`group/2222` 等 |
| Linux：routerhost、routerservice、routerconfig、routerrequest 完整包测试 | 通过 |
| Linux：routerhost、routerservice 完整包 `-race` 测试 | 通过 |
| Windows 本机：routerhost 完整测试包 | PASS |
| Windows 本机：名称接入/升级集成用例（新连接和旧连接） | PASS |
| Chromium/WebKit：Agents 接入与 Loomy 独立接入/恢复 | 4/4 通过 |
| 产品包 vet、Windows 全仓库 vet，以及最终两个修改包的 Linux/Windows vet | 通过 |
| 修改的 Go 文件 gofmt、diff 空白检查 | 通过 |
| Linux/Windows 端云协同 构建 | 通过 |

新增测试位于
[`host_model_names_test.go`](../internal/routerservice/host_model_names_test.go) 和
[`loomy_names_test.go`](../internal/routerhost/loomy_names_test.go)。覆盖同名模型的供应商区分、
嵌套/带点模型 ID、改名、别名、切换主模型、追加模型、事务失败回滚及离线恢复。

本轮没有运行上游 Magpie 全仓库测试或全部浏览器页面，也没有重启实际 Loomy 验收菜单。
浏览器测试使用独立配置目录和回环模拟上游；Windows 文件事务测试运行在本机。

## 构建与生效方式

Windows 候选文件：`magpie/dist/loomy-model-names-20261026/api-router.exe`。
SHA256：`DD2B8997075246F792B41FD835FF5A2394DE8CFBA536BBC0A083F2327C6F3BE8`。

候选由当前工作区构建，包含工作区此前已有的未提交改动。本次没有替换普通启动入口的
`magpie/dist/api-router.exe`，也没有修改实际 Loomy 配置或切换正在运行的服务。
使用包含修复的新构建后，在应用页重新连接 Loomy 即可更新名称；已缓存模型列表的 Loomy
需要重新打开。启动或切换版本时仍应使用项目入口返回的实际管理 URL。
