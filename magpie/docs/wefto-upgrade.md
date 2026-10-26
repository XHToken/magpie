# Wefto 织流：从 API-router 升级

Wefto 是本产品的新名称。新版本命令为 `wefto` / `wefto.exe`，源码入口为 `magpie/cmd/wefto`，Windows 安装目录默认为 `%LOCALAPPDATA%\Programs\Wefto`。Magpie 的原始 MIT 许可证与来源声明保留。

## 切换程序

1. 用旧 `api-router.exe dirs` 确认配置、数据、模型和状态目录，做好私有备份。凭证文件包含 API Key，不要上传到仓库或反馈附件。
2. 若旧版启用了登录自启，用该旧 EXE 执行 `service autostart disable`；再执行 `service stop`。保留旧 EXE 及其恢复记录。
3. 将同一 Wefto 发布版本的 ZIP、manifest、安装脚本和 SHA256SUMS 放在同一目录，按 README 安装到新的程序目录。安装器不会直接覆盖其他版本，也不会下载模型或启用自启。
4. 执行 `wefto.exe dirs` 检查所选路径，再执行 `wefto.exe service start`，打开输出中的 `managementURL`。已有模型、供应商、Profile、请求账本和宿主恢复备份继续使用。
5. 按需在应用页重连接入，重启 WorkBuddy / Loomy 以加载配置。需要登录自启时，再明确执行 `wefto.exe service autostart enable`。

## 配置和数据兼容

新安装使用 `wefto` 子目录：Windows 的 `%APPDATA%\wefto` 和 `%LOCALAPPDATA%\wefto`；Linux 使用各 XDG 根下的 `wefto`；macOS 使用 Application Support、Caches、Logs 下的 `wefto`。

当检测到旧 `api-router` 布局且没有新布局时，Wefto 会原地复用**整套**旧布局，不复制、移动或初始化另一套数据。旧目录名会继续显示在 `dirs` 中；这表示正在使用保留下来的原始数据。

如果新旧布局同时存在、路径无法读取或不是普通目录，启动会报错。先保留两套数据，确认要继续使用的完整备份，再由用户明确整理目录；程序不会猜测哪套凭证、模型或备份更新，也不会自动合并。锁和进程身份仍按原有规则验证，未知锁及其他程序占用的端口保持原样。

## 宿主与恢复

新接入使用 `Wefto` 显示名、`wefto` 供应商键和新的本地占位值。旧 `api_router` 供应商键、`API Router` 显示名及旧占位值仍受兼容识别；它们不是云端 API Key。端口同步保留已接入配置使用的键，重连通过原有预览、备份、应用与恢复事务更新配置，并保留首次接入前的原始快照。

旧版已生成的 Codex / Loomy 第一版适配器恢复记录可由 Wefto 读取；只接受已知的原始契约。之后的用户自定义字段仍会保留，冲突字段会阻止恢复。旧版程序包及其安装收据仍由旧 EXE 管理；Wefto 安装器只处理新包。

Windows 旧数据布局继续使用其旧自启动条目标识，以便识别而非另建重复项。若条目指向旧 EXE，先用旧 EXE 停用；程序不会抢占指向另一程序路径的条目。

浏览器在同一管理源下会读取旧 UI 偏好和引导进度，后续保存使用 Wefto 的键。管理握手保留旧请求头兼容，同时保持原有同源、令牌和进程身份检查。

## English

Stop the previous service using its original executable, disable its login autostart if enabled, and keep private backups of all directories reported by `api-router.exe dirs`. Install Wefto into its own program directory and inspect `wefto.exe dirs` before starting it.

Fresh installations use `wefto` directories. If only legacy state exists, Wefto reuses that whole layout in place, preserving credentials, model downloads, request records and host recovery backups. Competing layouts or unknown paths stop startup for explicit reconciliation. No data is moved or merged automatically.

Fresh host connections use Wefto names. Known legacy provider IDs, local placeholders, management headers and frozen version-1 recovery contracts remain compatible. Reconnection retains the first backup and preserves unrelated host edits. Existing autostart entries remain bound to the exact executable they own; disable an old entry with the old executable before enabling Wefto. Old packages remain managed by their original installer.
