# 端云协同统一更名

项目展示名称统一为“端云协同”，所有界面语言都使用这个名称。

- 管理页标题、导航品牌、关于页面、教程、服务控制、用量和应用接入提示使用新名称。
- 顶部标志和 favicon 使用“端”字，图标地址增加新版本标记，刷新旧图标缓存。
- Codex、Loomy 的供应商展示名以及 WorkBuddy 的默认标签使用新名称；具体模型名、路由组名和用户别名沿用各自名称。
- 已有 Loomy 连接可以使用旧品牌标签重新连接，并在原有事务中升级展示名；首次备份和离线恢复仍然保留。WorkBuddy 同时识别新旧默认标签。
- 中英文 README、使用说明、设计文档、子系统文档、启动输出及仓库内技能展示名同步更新。

可执行文件名、发布包名、发布标签、服务身份、HTTP 认证头、配置键、模型 ID、浏览器偏好键和数据目录保留兼容的技术标识。旧的品牌恢复报告保留为历史记录，不代表当前命名。

## 验证

更新后的 `TestConsoleServesCollaborationNameAndIcon` 在修改产品资源前失败，明确指出 HTML 标题、品牌标志、图标 URL 和实际 SVG 响应仍为旧品牌；修改后通过。

Windows PowerShell 启动脚本隔离测试和技能安装器的 13 项 Python 测试通过。包含中文提示的 PowerShell 脚本使用 UTF-8 BOM，已确认 Windows PowerShell 5.1 输出“端云协同”而无乱码。

`routerhost`、`routerservice` 和 `cmd/wefto` 的 Go 竞态测试、对应静态检查以及 Windows/Linux 构建通过。Go 格式检查、产品 JavaScript 语法检查和 `git diff --check` 通过。Windows 原生 Chrome 页面回归通过，覆盖五种语言、三种窗口宽度以及实际图标响应。

浏览器回归使用当前匹配构建和临时配置目录。WSL 读取 Windows 目录较慢，后续检查转到内容一致的 Linux 临时副本；不修改真实宿主配置。本次未运行上游 Magpie 全仓库测试或 macOS 原生测试。

Chromium 的相关回归全部通过：Agents 和本地模型两项在初次运行通过，其余页面、Loomy、路由组、服务控制和教程在临时副本中 20/20 通过。WebKit 对同一组功能完整运行 22/22 通过，覆盖五种语言、桌面和移动布局。

项目默认程序 `magpie/dist/api-router.exe` 已更新为 `dev+duanyun-20261027`，SHA256 为 `7F536360E4EFAAF6D386B9C957435EB6C4CD7E0DBFA694AB6324BE6130A110C5`。替换前确认该项目程序没有运行，旧程序保存在 `tmp/duanyun-rename/wefto-before-rename.exe`。

## 已安装实例

本机已有独立安装的发布版位于 `%LOCALAPPDATA%\Programs\Wefto\wefto.exe`。它与本仓库的 `magpie/dist/api-router.exe` 是不同的程序；项目启动入口的只读检查正确拒绝接管该实例。本次源码更名不会自动替换独立安装的发布版，或改写其宿主绑定。
