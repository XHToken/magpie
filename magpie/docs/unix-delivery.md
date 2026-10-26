# Wefto macOS / Linux

## 中文

这是包含内嵌管理网页的独立程序，无需安装 Go、Node 或 Python。
从 https://github.com/XHToken/magpie/releases 下载对应系统和架构的包：

| 系统 | 包后缀 |
| --- | --- |
| macOS Apple Silicon（M 系列） | darwin-arm64.tar.gz |
| macOS Intel | darwin-amd64.tar.gz |
| Linux x64 | linux-amd64.tar.gz |
| Linux ARM64 | linux-arm64.tar.gz |

解压后在文件夹内运行 `./start.sh`。macOS 也可以双击 `start.command`。
保留终端窗口，在浏览器打开终端打印的 `Management:` 地址；端口被占用时
程序会选择其他端口。按 Ctrl+C 停止。也可以运行 `./wefto --help` 查看命令。
不需要 sudo，不会自动修改其他应用的连接设置。

可在解压前对照同一 Release 的 SHA256SUMS：macOS 用 `shasum -a 256 文件名`，
Linux 用 `sha256sum 文件名`。校验文件与 manifest 同时发布。
macOS 包没有 Developer ID 签名或公证；首次启动若被系统阻止，请确认下载来源后
在“系统设置 → 隐私与安全性”中允许打开，再次启动。不要关闭系统整体安全保护。

配置和数据保存在用户目录，不保存在解压目录。macOS 默认使用
`~/Library/Application Support/wefto`，Linux 遵循 XDG 配置/数据/缓存目录；
`./wefto dirs` 显示实际路径。若检测到完整的旧 API-router 数据布局会沿用它。
升级前停止旧进程，备份这些目录，再使用新解压目录启动；不要同时运行两个版本。
包内不含模型权重或推理运行时，可在管理页面按需配置云端或下载本地模型。
各平台的实际验证结果以对应 Release 的发布说明为准。提供预编译包不代表
所有宿主或所有平台的本地推理后端均已通过兼容验证。

## English

Download the archive for your OS and CPU from the same GitHub Release.
The executable embeds the management UI; Go, Node and Python are not required.
Extract the archive and run `./start.sh`, or double-click `start.command` on macOS.
Keep the terminal open and visit its printed `Management:` URL. An occupied port
causes automatic selection of another loopback port. Stop with Ctrl+C.
No sudo is needed. Other applications are connected only when explicitly configured.

Compare `shasum -a 256 ARCHIVE` (macOS) or `sha256sum ARCHIVE` (Linux) with
the release's SHA256SUMS before extracting. macOS downloads are not Developer ID
signed or notarized. If Gatekeeper blocks opening, verify the source and approve
this program in System Settings → Privacy & Security, then launch it again.

`./wefto dirs` reports the actual user configuration and data directories.
Stop the old version and back up those directories before upgrading. Existing
complete API-router layouts are reused. Archives contain no model weights or
inference runtimes; configure cloud providers or download models in the UI.
See the release notes for the platforms actually tested. Prebuilt archives do
not certify every host integration or inference backend.
The MIT license and upstream attribution are included in LICENSE.
