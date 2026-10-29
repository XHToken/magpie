# 端云协同

[简体中文](README.md) | [English](README.en.md)

端云协同是运行在本机的 AI 模型路由服务。在一个网页中管理云端 API、本地小模型和路由策略，再将 WorkBuddy、Loomy 接入同一服务。项目基于固定版本的 Magpie 源码改造，产品入口为 `magpie/cmd/wefto`。

**[下载 macOS / Linux / Windows 预览版](https://github.com/XHToken/magpie/releases/tag/wefto-v0.1.0-beta.13)** · [浏览端云协同源码](https://github.com/XHToken/magpie/tree/main)

当前版本为 **0.1.0-beta.13**，优先适配 WorkBuddy 与 Loomy 的本机模式。提供 Windows x64、macOS Apple Silicon / Intel、Linux x64 / ARM64 预编译包。仓库中的其他预置应用和协议适配器不表示全部宿主、版本、平台或模型已经通过兼容验收。

## macOS / Linux 解压启动

从上面的 Release 下载对应架构的 `*.tar.gz`，校验 SHA256 后解压，进入文件夹运行 `./start.sh`。macOS 也可双击 `start.command`。无需 Go、Node 或 Python；打开终端显示的管理地址，按 Ctrl+C 停止。详见 [Unix 使用说明](magpie/docs/unix-delivery.md)。macOS 包未进行 Developer ID 签名或公证，首次启动可能需要在系统设置中允许打开。

## 下载与安装（Windows x64）

使用安装包无需安装 Go、Node 或 Python。打开上面的发布页，将同一版本的以下四个文件下载到同一个本地文件夹：

| 文件 | 用途 |
| --- | --- |
| `wefto-0.1.0-beta.13-windows-amd64.zip` | 程序、内嵌管理页面和安装/卸载说明 |
| `wefto-0.1.0-beta.13-windows-amd64.manifest.json` | 安装器使用的文件大小与 SHA256 清单 |
| `install-windows.ps1` | 独立安装脚本 |
| `SHA256SUMS` | ZIP、manifest 和独立安装脚本的 SHA256 校验值 |

在该文件夹中打开 PowerShell，执行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install-windows.ps1 -ManifestPath "$PWD\wefto-0.1.0-beta.13-windows-amd64.manifest.json" -PackagePath "$PWD\wefto-0.1.0-beta.13-windows-amd64.zip"

$router = Join-Path $env:LOCALAPPDATA 'Programs\Wefto\wefto.exe'
& $router service start
```

安装器会检查包和每个文件的 SHA256，默认安装到 `%LOCALAPPDATA%\Programs\Wefto`。打开启动输出中的 `managementURL` 即可使用；安装不会启用登录自启或下载模型。此预览包尚未进行发布者代码签名，也没有自动更新功能。

已有安装的用户请先阅读[手动版本替换与备份说明](magpie/docs/windows-delivery.md#手动版本替换备份与恢复)，安装器不会直接覆盖其他版本。

## 首次使用

1. 打开管理页面，在供应商页添加 API Key、API 地址及模型，执行连接检查。
2. 按需建立路由组，选择云端模型或本地模型及调度策略。
3. 在应用页为 WorkBuddy 或 Loomy 选择模型/路由组并开启接入，重新打开宿主以加载配置。
4. 如需本地推理，先在本地模型页完成设备检测、模型与运行时安装及能力检查，再显式启用。

本地模型和云端供应商可分别使用。服务默认允许实验性脱敏；是否执行仍由路由组或应用开关控制，模型需单独启用。可使用 `--no-experimental-redaction` 显式关闭启动许可。

## 当前功能

- 云端供应商：保存 API Key、地址、协议和模型；由用户主动获取模型或执行连接检查。应用通过独立 Profile 绑定供应商与模型。
- 路由组：支持智能端云分流、顺序、权重和手动策略及失败回退；应用可以同时绑定多个模型和路由组。
- 云端请求：支持 OpenAI Chat、OpenAI Responses、Anthropic Messages 的受限协议契约，提供有限重试、流式响应、请求观测和用量统计。工具调用及协议转换仍取决于客户端输入与上游能力。
- 本地模型：提供设备检测、固定 Spark-X2.5-1.7B / 4B 模型与运行时下载、单推理进程管理、上下文和阶段预算设置。用户需明确启用、安装并完成能力检查，才可选择实验性判断与执行；样本检查通过不代表模型质量正式验收。
- 脱敏：具备本地识别、严格结果校验、映射及响应还原流程。识别质量尚未正式验收；实验性试用需要显式启动参数，校验失败会阻断云端请求。
- 宿主接入：提供 WorkBuddy、Loomy 的配置适配、变更预览与独立恢复命令；兼容范围见[宿主配置文档](magpie/docs/subsystems/wefto-host-config.md)和[Loomy 文档](magpie/docs/subsystems/wefto-loomy.md)。
- 本机管理：内嵌网页、新手指南、服务启动/停止/重启、Windows 每用户登录自启和保留恢复工具的卸载流程。

服务默认尝试 `127.0.0.1:13145`。端口被占用时自动绑定其他可用回环端口，保存实际地址，并同步已接入 Router 的 WorkBuddy、Loomy 配置；后续启动继续使用保存的端口。同步失败会撤回本次宿主修改并报告启动失败。首次启动不会下载、启用或加载模型；关闭网页不会停止服务。浏览器通过同源握手自动连接管理接口，命令行管理客户端使用启动时报告的私有令牌文件。云端请求使用应用绑定的供应商凭证，管理令牌不能用作模型请求凭证。

## 从源码运行

需要 `magpie/go.mod` 指定的 Go 版本（当前为 **1.26.8**）。产品二进制内嵌网页，普通构建不需要 Node 或桌面 WebView。

在仓库根构建 Windows 版本：

```powershell
go -C magpie build -trimpath -o dist/wefto.exe ./cmd/wefto
.\magpie\dist\wefto.exe serve
```

日常启动建议在仓库根执行 `start.cmd`，或运行
`powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/start-project.ps1 -NoBrowser`。
此入口固定使用 `magpie/dist/wefto.exe` 和 Windows 用户的正常配置目录，
重复运行复用已有服务，并检查保存的监听地址、实际实例和 WorkBuddy 接入地址。
只检查、不启动可添加 `-CheckOnly`。确认已退出或 PID 被复用的本产品残留锁会被完整归档；
活跃实例和未知锁保持原样，其他程序占用的端口会被自动避开。
历史测试使用的临时端口和专用 APPDATA / LOCALAPPDATA 不属于此入口；
不要从历史测试目录选择 EXE，或只改宿主端口来迁就另一套实例。
请使用启动入口打开实际管理地址；若已运行的 WorkBuddy 或 Loomy 缓存了旧地址，
端口变更后重新打开应用，让它加载已同步的配置。

macOS / Linux：

```sh
go -C magpie build -trimpath -o dist/wefto ./cmd/wefto
./magpie/dist/wefto serve
```

也可以执行 `make -C magpie wefto`。`make build`、模块根目录的 `go build .`、原 Dockerfile 构建的是上游 Magpie；Wefto 使用 `./cmd/wefto`。

打开启动输出中的管理地址，在供应商页添加云端供应商，再到应用页绑定模型并检查连接。本地模型和脱敏分别管理，按页面状态完成各自的检查。

常用命令（将 `wefto` 替换为本机二进制路径）：

```sh
wefto --help
wefto dirs
wefto config check
wefto healthcheck
wefto service --help
wefto host --help
```

前台 `serve` 使用 `Ctrl+C` 停止。Windows 安装版也可以使用 `service start` 在后台启动；服务管理和恢复说明见 [Windows 使用文档](magpie/docs/windows-delivery.md)。

显式试用实验性脱敏：

```sh
wefto serve --experimental-redaction
```

试用仍需在页面中配置并安装相应模型，不能据此将识别结果视为可靠隐私保护。详细边界见[脱敏文档](magpie/docs/subsystems/wefto-redaction.md)。

## 测试与打包

产品统一回归需要 Go、Node（基线 **22.23.2**，支持 22 / 24）及锁定的 Playwright 浏览器。先按[回归文档](magpie/docs/product-regression.md)准备依赖，再从仓库根执行：

```sh
node magpie/scripts/product-regression.cjs
```

入口包含产品构建、产品及共享包的 race / vet、Chromium 与 WebKit 页面测试；任一步失败返回非零。旧 `make test-ui` 覆盖上游 Magpie GUI，产品页面使用上述统一入口。自动化回归使用隔离目录与模拟上游，不能替代真实模型、真实宿主及各平台验收。

Windows x64 固定版本包由 `magpie/scripts/package-windows.ps1` 生成，必须提供明确的语义版本；安装器只接收本地包。预览包发布在[本仓库的 Releases](https://github.com/XHToken/magpie/releases)，安装器不会自行查询或下载 latest。用法、校验和安装/卸载边界见 [Windows 交付文档](magpie/docs/windows-delivery.md)与[交付子系统](magpie/docs/subsystems/wefto-delivery.md)。

## 代码与文档

- [产品服务与命令](magpie/docs/subsystems/wefto-service.md)
- [应用 Profile](magpie/docs/subsystems/wefto-apps.md)、[供应商](magpie/docs/subsystems/wefto-providers.md)、[云端协议](magpie/docs/subsystems/wefto-cloud.md)
- [本地模型与运行时](magpie/docs/subsystems/wefto-runtime.md)、[脱敏](magpie/docs/subsystems/wefto-redaction.md)、[存储](magpie/docs/subsystems/wefto-storage.md)
- [子系统索引](magpie/docs/subsystems/README.md)

`magpie/` 同时保留上游源码、测试与资料，其中部分能力没有接入 Wefto 的产品生命周期。`magpie/README.md`、`magpie/README.zh-CN.md` 是上游项目归档，不作为 Wefto 的安装或发布入口。上游站点 `magpie/site/` 和旧工作流 `magpie/.github/` 不随当前公开源码快照分发；归档图片与相关文档使用固定基线的上游链接。历史开发记录和本机验收产物也不随公开源码分发；公开文档中的历史记录描述不代表当前版本重新完成过验收。

## 来源与许可证

本项目导入的 Magpie 基线为 [`707fbb249df755423d3b2b77c6f001dd376d75c4`](https://github.com/yetone/magpie/commit/707fbb249df755423d3b2b77c6f001dd376d75c4)。本地导入记录中的来源仓库为 [`xyzane54/magpie`](https://github.com/XHToken/magpie)，原始项目与模块路径为 [`yetone/magpie`](https://github.com/yetone/magpie)。Wefto 与上游 Magpie 的产品发布分别管理。

根目录 [LICENSE](LICENSE) 原样保留 Magpie 的 MIT 许可证和 `Copyright (c) 2026 yetone` 声明，与 [magpie/LICENSE](magpie/LICENSE) 一致。保留的第三方组件及资源以其自身许可证和声明为准；模型与推理运行时通过产品配置的来源另行下载，不包含在本源码仓库中。
