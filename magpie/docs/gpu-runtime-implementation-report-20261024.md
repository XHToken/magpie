# 本地脱敏推理加速实现与验证报告

日期：2026-10-24。范围：Windows CUDA / Vulkan / CPU 自动选择，以及 macOS Apple Silicon / Intel 兼容。

## 已实现的行为

| 设备 | 选择顺序 | 回退行为 |
| --- | --- | --- |
| Windows x64，NVIDIA 驱动可见 | CUDA → Vulkan（驱动可见时）→ CPU | GPU 组件缺失、驱动不可用、内存不足、加载或验证失败时尝试下一后端 |
| Windows x64，AMD / Intel 等支持 Vulkan 的设备 | Vulkan → CPU | 同上 |
| macOS 14+，Apple Silicon（M 系列） | Metal → CPU | Metal 不可用或实际卸载验证失败时用 CPU |
| macOS 14+，Intel | CPU | 本轮未实现 Intel Mac 的 Metal 加速 |
| Linux x64 | CPU | 本轮未扩展 Linux GPU |

这里的 Apple 支持是桌面 macOS；没有新增 iPhone / iPad 应用。

程序不再仅根据“检测到显卡”宣布 GPU 加速。它先校验固定版本运行时及依赖，再由该运行时枚举设备、读取单卡可用内存，加载模型并检查实际 GPU 层数和设备缓冲区，最后执行固定的短 JSON 推理。通过后才显示 GPU 或 CPU/GPU 混合推理。

选择时保留至少 1 GiB 或设备报告总内存的 15%，采用自动层数适配，允许部分层卸载；不累加多卡显存，也不把集成显卡共享内存当成独立显存。64K 服务上下文保持显式设置，加载失败仍可回退 CPU。实际是否能在某台机器装下 64K，需要该机器运行验证。

## 安装、超时与故障恢复

- 明确执行安装/修复时，先准备 CPU，再准备驱动提示对应的 GPU 组件。GPU 下载失败保留可用的 CPU 安装。
- 普通启动、状态查询、启用和加载不会偷偷下载组件。已有 CPU 安装需执行一次安装/修复才能补齐 Windows GPU 运行时；Apple Silicon 的 CPU 和 Metal 复用同一固定归档。
- 冷加载继续共享原有 90 秒预算；单次 GPU 枚举最多 8 秒，GPU 加载/验证最多 45 秒或剩余时间的一半，为后续回退留出时间。
- 旧子进程确认退出后才启动下一候选，不并行占用两份模型。GPU 加载/验证失败进入 10 分钟冷却，明确修复组件后可重新尝试。
- GPU 子进程异常退出会让当前调用失败；后续加载可回退 CPU。脱敏输出格式错误或漏检不被当成驱动问题重复推理，也不会因此放行未脱敏原文。
- 脱敏仍为此前确定的 30 秒共享期限，覆盖排队和所有提取批次。此次没有再次扩大该期限。

## 关键修改与修复

1. **运行时目录及安装器**：增加固定 b10828 CUDA、Vulkan、macOS CPU 包；CUDA 附带独立固定的 CUDA DLL。多依赖组合保留原始归档、解压及组合校验，逐文件核对 SHA256、PE 架构和版本。
2. **进程层**：CPU 明确 `--device none`、零层卸载；GPU 明确设备、层数适配和内存预留。过滤继承的推理/动态库加载覆盖变量，macOS 同样适用。
3. **实际卸载证据**：实测发现 b10828 默认日志级别不输出卸载行，导致“GPU 实际工作却被判未验证”。已固定 `--log-verbosity 4` 并关闭颜色，仅解析有界启动输出中的数值证据，健康就绪后停止采集，不保存模型输入输出。
4. **硬件门禁**：GPU 能力尚未验证时，单独评估合法的 CPU 安装方案，保留原始硬件事实；不跳过 RAM、剩余内存、CPU、系统架构或磁盘检查。
5. **绑定和页面**：实际后端、设备、卸载层数、上下文参与执行身份及脱敏绑定。后端变化重置能力证据和旧缓存；页面显示真实后端、层数及回退状态，卸载或失去健康状态后不继续声称正在加速。
6. **安装损坏恢复**：修复自动模式下文件后来损坏、加载失败却仍显示“已安装”的边界问题。所有候选校验失败时清除安装状态，保留修复入口，不启动进程、不自动下载；既有加载失败返回码保持不变。

主要代码：`internal/routerruntime/backend.go`、`internal/routerprocess/accelerator.go`、`internal/routermanifest/accelerators.go`、`internal/routerdevice/cpu_fallback.go`、`internal/routerdownload/assembly.go`，以及 runtime / redactor / local-model 页面集成。

## 已完成验证

### Windows 本机真实 Vulkan 闭环

使用隔离数据目录和固定归档，不使用或改写日常服务配置。模型为 Spark-X2.5-1.7B Q4_K_M，64K 上下文，输入为约 2,000 字的固定合成文本。

| 项目 | 结果 |
| --- | --- |
| 实际设备 | Intel Arc 130T GPU（运行时报告 16GB） |
| 自动选择 | Vulkan，实际卸载 29 / 29 层 |
| 最终复测加载耗时 | 4,216 ms |
| 最终复测长文本脱敏耗时 | 3,910 ms |
| 指定敏感值残留 | 无 |
| 响应还原 | 通过 |
| 严格运行时/脱敏绑定 | 一致 |
| 关闭清理 | 无关闭错误，最终无模型 PID |

这说明本机真实 GPU 路径可用，不代表所有模型、硬件、文本都能达到同样速度或检出率。没有据此宣称固定倍数加速，也没有重新判定已知的 1.7B 漏检问题。

### CUDA、Apple 与自动化回归

- Windows CUDA 原始引擎和依赖归档已下载并核对固定 SHA256；真实安装、组合及 9 个 VC/CUDA 依赖的校验通过。本机没有 NVIDIA GPU，因此尚未完成真实 CUDA 模型推理。
- 两份 macOS 原始归档已核对固定 SHA256；Apple Silicon、Intel macOS 应用交叉编译及相关包 vet 通过。没有 Apple 实机，Metal 枚举、实际卸载和推理仍需实机验收。
- 最终 9 个相关 Go 包通过 `-race` 测试，共 1,571 项测试及子测试，0 失败。Linux、Windows x64、macOS arm64/x64 的 端云协同 构建及相关包 vet 通过。
- 本地模型页面通过 Chromium 和 WebKit，覆盖五种语言、桌面/手机布局、混合卸载展示及健康状态失效。
- 驱动不可用、加载失败、零层卸载、合成推理无效、显存不足、组件缺失均覆盖自动 CPU 回退，检查不会重叠运行模型。
- 反向验证：临时去掉实际卸载检查后，`TestAutomaticBackendFailureFallsBackWithoutOverlappingChildren/zero-offload` 明确失败；恢复实现后通过。变异仅用 Go overlay，没有改写生产源码。
- 安装损坏回归 `TestAutomaticBackendClearsInstallationAfterAllFilesFailVerification` 通过，移除修复的 overlay 会明确复现“安装状态未清除”。首次测试曾错误地要求改变既有加载失败返回码，已修正为检查失败、安装状态及无下载/无子进程；中间失败日志保留，随后重跑相关包。

### 仓库全量检查

Windows 全仓 `go vet ./...`、Linux 全仓 `go vet -tags nogui ./...`、Linux 无界面主程序构建、Windows 主程序构建通过。普通 Linux GUI 的无 cgo 构建/vet 遇到现有 Wails `undefined: pointer` 编译约束；独立 端云协同 四平台构建不依赖该 GUI，并已通过。

`go test -tags nogui ./...` 原始运行：14,137 项测试及子测试通过、5 项失败、187 项跳过。不能把相关包通过写成整个仓库全部通过。

| 未通过项 | 已核实的问题与处理范围 |
| --- | --- |
| `internal/agent.TestGrokInstalled` | 测试安装的是 `MZ` 占位文件，模拟版本读取未覆盖更新后的真实执行检查；WSL 返回 `Invalid argument`。本轮未修改 Grok 更新模块。 |
| `internal/gateway.TestCommandCodeGoSaysTheAgentsFolder` | 普通临时目录被 `provider.GitOnDisk` 判断为 Git 仓库，与测试期望不符；本轮未修改此模块，具体目录来源需独立定位。 |
| `internal/provider.TestZCodeEnvironmentSaysOnlyWhatIsKnown` | 同样因普通临时目录的 Git 仓库判断与预期不符而失败；本轮未修改此模块。 |
| `internal/proc.TestNoCommandBypassesProc` | 扫描到了 `dist/redaction-budget30-20261023/candidate/` 内历史源码副本的正常 `proc` 实现，副本路径不在守卫豁免目录中。 |
| `internal/pluginrt.TestPluginsImportOnlyCore` | 同一扫描误纳入历史副本的 `main.go` / `plugins/all`，被误当成核心包导入插件。 |

三项产品测试失败在上一轮预算修复报告中已有记录，本轮仍保留，未绕过或改写测试。两项目录扫描守卫在不包含构建产物的隔离源码副本中补跑均通过，原始失败日志保留。守卫补跑没有更改测试或生产实现。

补充静态检查过程中，第一次 Linux 无界面 vet 因验证脚本在读取期间被改写而出现 shell 语法错误；它不是生产源码编译错误，原日志另存 `full-vet-linux-first.log`，随后以固定脚本重跑通过。

## 交付与待验收

代码与隔离验证构建已完成，未替换日常使用的 `magpie/dist/api-router.exe`，未调整 WorkBuddy/Loomy 连接端点，未修改用户的模型/提供方配置。

下一步实机验收重点：NVIDIA Windows 的 CUDA 加载/长文本推理、M 系列 Mac 的 Metal 加载/CPU 回退，以及 Intel Mac 的 CPU 加载。应分别记录模型、上下文、实际后端、层数、冷加载/推理耗时及脱敏结果，不能仅凭交叉编译视为验收通过。

本地证据位于 `magpie/dist/gpu-runtime-20261023/`（忽略目录）：`final-validation.json`、`final-pass-summary.json`、`tests.log`、`ui.log`、`native-result.json`、`cuda-install-result.json`、`mutation.log`、`verification-negative.log`、`runtime-final.log`、`clean-guards.log` 及全量检查日志。归档目录名沿用任务开始日期，实际最终验证日期为 2026-10-24。
