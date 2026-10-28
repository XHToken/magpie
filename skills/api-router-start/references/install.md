# 下载、安装与启动

进入安装/启动分支后，按用户请求和现有授权完成下载、安装和启动。默认先启动已有安装；仅在用户明确要求更新或指定其他版本时准备新版本。不为已授权的普通安装重复请求确认。

源码：[XHToken/magpie，main 分支](https://github.com/XHToken/magpie/tree/main)。发布来源仅限该仓库 `wefto-v*` Releases。说明中项目称为“端云协同”，既有发布包使用 `wefto` 技术标识，命令为 `wefto`，源码入口为 `magpie/cmd/wefto`；保留本 skill 的 `$api-router-start` 调用名。不使用旧仓库或上游 Magpie 安装器。

每次需要下载安装时，实时查询 [项目 Releases](https://github.com/XHToken/magpie/releases)，按 `published_at` 从新到旧筛选 `wefto-v*`、非草稿、当前系统/架构的发布。默认包含预览版，不使用仅面向正式版的 `/releases/latest` 代替筛选。默认不传 `-Tag` 或 `--tag`，不固定任何历史版本。以下为已支持的包类型，具体版本号及文件 URL 始终取自本次查询结果：

查询入口为 `https://api.github.com/repos/XHToken/magpie/releases?per_page=100&page=1`，有后续页时继续读取，再按发布时间筛选。从选中 Release 的 `assets` 中识别匹配系统/架构的项目包及配套文件，使用各附件返回的 `browser_download_url` 下载；不从本 skill、README 示例、历史验收或缓存复制固定包地址。包名模板只用于识别附件，不能代替实时发现。每次新的下载任务都重新查询，不把本次查到的版本写回默认值；查询失败时报告原因，不能改下固定旧包。此规则针对项目程序包，本地模型权重仍遵循产品目录的版本和校验要求。

| 系统 | 架构 | 包后缀 |
| --- | --- | --- |
| Windows | x64 | `windows-amd64.zip` |
| macOS 12+ | Apple Silicon（M 系列） | `darwin-arm64.tar.gz` |
| macOS 12+ | Intel | `darwin-amd64.tar.gz` |
| Linux | x64 | `linux-amd64.tar.gz` |
| Linux | ARM64 | `linux-arm64.tar.gz` |

## 入口

识别真实 OS 和 CPU 架构。Windows ARM 不冒充 amd64，WSL 是独立 Linux 环境。若用户只是启动本项目源码工作区，且仓库有 `scripts/start-project.ps1`，遵循仓库 AGENTS.md，运行该入口 `-NoBrowser`。该入口优先通过正常用户布局中的实例记录复用标准安装的原程序，避免源码与安装版争用同一配置；没有记录时默认使用当前源码构建。明确要验证源码构建时用 `-UseProjectBuild`，遇到运行中的其他构建仍按原生保护报告冲突，不自动停服务。明确要求安装发布包时使用下文。

以下 SKILL_DIR 指 skill 根目录（含 SKILL.md），不是 references 目录；执行时替换为实际绝对路径。

**Windows x64，PowerShell 5.1+：**

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File '<SKILL_DIR>\scripts\start-api-router.ps1' -NoBrowser
```

复用 `%LOCALAPPDATA%\Programs\Wefto`；未安装则选择最新发布且附件完整的兼容版本（包含预览版），下载同一 Release 的 ZIP、manifest、安装器、SHA256SUMS，校验后调用官方安装器，再 `service start`。无需 Go、Node、Python 或管理员权限。

默认未找到端云协同时，若存在标准路径的旧 API-router 安装，先复用它；不隐式更名升级。用户明确要求切换新仓库版本时按更新文档迁移。

Windows 默认启动与只读检查会先读取正常用户目录的实例记录，只接受标准安装目录中的 Wefto/API-router（项目入口还接受自身构建）。记录只用于选择 CLI，随后仍由该 CLI 核验进程和认证身份。未知程序、损坏/缺失记录、双布局冲突会停止并保留现场，不再创建隔离数据目录、安装另一个副本或下载模型。指定版本/程序目录仍保持显式选择，不偷偷改用另一个构建。

- `-Plan`：仅检查安装或查询候选 Release，不下载、不启动。
- `-DownloadOnly`：仅下载和校验，输出版本及缓存路径。
- `-InstallOnly`：安装但不启动。
- `-CheckOnly`：只检查已有安装，不安装/启动。
- `-Tag wefto-vVERSION`：固定版本，不覆盖其他已安装版本。
- `-StableOnly`：排除预览版；无稳定包则报告，不静默改用预览版。
- `-InstallDir ABSOLUTE_DIR`：指定程序目录，不与配置/数据重叠。

**macOS / Linux，amd64 / arm64：**

```sh
python3 '<SKILL_DIR>/scripts/prepare-unix.py' --plan
python3 '<SKILL_DIR>/scripts/prepare-unix.py' --no-browser
```

自动助手使用已有 Python 3.9+（可用 Codex 提供的解释器），复用 `~/.local/opt/wefto/wefto`。未安装时优先下载同一 Release 的匹配 `.tar.gz`、`.manifest.json` 和 `SHA256SUMS`，验证包及 manifest 的 SHA256，再逐文件校验归档路径、大小、权限、哈希及 ELF/Mach-O 架构后原子安装。macOS 在 Rosetta 下也优先识别原生 Apple Silicon。下载失败或包损坏明确报错，不改用源码绕过校验。

应用包本身无需 Go、Node 或 Python。若环境没有现成 Python，用现有下载工具、系统 `shasum -a 256`（macOS）/`sha256sum`（Linux）与 `tar` 完成相同校验，再在独立程序目录运行包内 `./start.sh`；macOS 也支持 `start.command`。不为安装预编译包要求用户安装开发工具。

只有对应系统/架构确实没有发布包时才从固定源码构建：固定 API 返回的 `main` 提交，或用户指定的端云协同 Release 标签，只构建 `magpie/cmd/wefto`。此时需 Git 和源码 `go.mod` 要求的 Go，允许 Go 下载自身工具链，不调用系统包管理器。缺依赖时说明缺什么及下一步。

未指定目录/版本且没有端云协同时，也可复用 `~/.local/opt/api-router/api-router` 的旧安装。需要更名升级时按更新文档处理，不合并新旧配置根。

参数：`--plan` 仅查询、`--download-only` 仅下载校验、`--prepare-only` 仅安装/构建、`--check-only` 仅检查、`--stable-only` 排除预览版、`--tag wefto-vVERSION`、`--install-dir ABSOLUTE_DIR`、`--no-browser`。默认启动并验证后台进程，返回包标签（源码构建时返回提交）、版本、程序位置及实际管理 URL。手动运行 `./start.sh` 为前台模式，保持终端打开，Ctrl+C 停止。

默认获取最新兼容包：`python3 '<SKILL_DIR>/scripts/prepare-unix.py' --download-only`；Windows：`powershell.exe -NoProfile -ExecutionPolicy Bypass -File '<SKILL_DIR>\scripts\start-api-router.ps1' -DownloadOnly`。用户明确指定版本时才添加标签参数。已有其他版本需更新时按更新文档处理，不直接覆盖。

“最新兼容”以发布时间为准，不按模型名、标签字典序或列表第一项猜测。跨页汇总后统一筛选；排除草稿及其他产品，可按用户 `StableOnly` 选择排除预览版。缺少该平台附件可查下一兼容发布；已存在包但配套校验不完整、查询/下载失败时明确报错或解释，不能偷偷使用上次缓存冒充最新。选定后冻结本次标签，完整校验同版本文件；未来再次下载时重新查询。包结构/接口改变时按新版本文档适配，不执行不认识的安装协议。

Go 工具链或依赖下载失败时报告具体地址和错误。可为本次命令使用用户已有代理/可信镜像（例如通过进程环境传入 GOPROXY），不永久更改全局 Go 配置，不关闭 GOSUMDB 或 HTTPS 校验，不以降低 go.mod 版本规避失败。

macOS 当前源码不支持跨进程 `service start/status/stop` 身份校验，脚本使用独立 `serve` 进程，并通过实例记录、PID、healthz 和带令牌管理接口核验。不要声称支持 Windows 的完整服务管理或自启。无法核验旧锁时保留并报告。

macOS 包未做 Developer ID 签名或公证。若 Gatekeeper 实际阻止运行，提示核对来源后在系统“隐私与安全性”中允许该应用再重试，不关闭系统整体保护。macOS 原生运行尚未由本 skill 的本机验收覆盖，不能将格式/架构检查称为原生启动验证。

## 约束

- 使用正常用户配置/数据根；不从 `z-steps/`、其他 worktree 或历史临时目录选择 EXE/端口，不把测试用 APPDATA/LOCALAPPDATA 留给日常实例。
- 使用实际启动结果中的 HTTP 回环管理 URL，验证产品、PID 及认证身份。令牌不输出、不分享、不发给非回环地址。
- 原生服务自动避开被占用端口。不得杀占用者、手动删除未知锁、重置配置或改宿主端点以迁就另一实例。宿主不一致时检查实时服务、配置根、恢复记录，再按对应版本文档处理。
- 保留模型、供应商、API Key、宿主设置及恢复记录。安装/启动不代替用户选择供应商、接入宿主、下载模型、启用实验脱敏或登录自启。
- 明确请求更新时先读 [update.md](update.md)，完成备份与替换；普通启动不隐式更新。
- GitHub 限流或下载不可达时报告具体阶段，保留已有安装；不反复请求、不切换其他仓库、不把网络错误当作缺少平台包。下载地址失败可通过同仓库官方 asset API 重试一次，完整性校验保持一致。

验证成功后打开或提供真实管理 URL，报告版本、程序路径和复用/安装/构建状态。首次使用提示配置供应商，再从应用页接入 WorkBuddy/Loomy。失败报告具体阶段及下一步，不把下载完成称为启动成功。SHA256 是完整性校验，不是发布者代码签名。

当前任务是带用户学习时，准备好环境后回到 [首次使用](first-use.md) 的相应步骤，让用户亲手配置供应商和应用；用户仅要求启动时，报告实际入口并解释下次如何自行启动/停止即可。
