# 端云协同 0.1.0-beta.8

本版本改进本地模型下载、进度显示和已有服务接续，并保持 Wefto 命令、目录与 API-router 兼容标识。

- 本地模型下载按固定清单在备用来源之间回退，继续校验文件大小与 SHA256。
- macOS 按实际可用内存评估本地模型资源，避免将总内存当作当前余量。
- WorkBuddy 使用可读模型名称作为选择项，并在请求进入时还原为准确的模型目标。
- Windows 项目入口与安装引导复用已记录的标准安装服务，核对鉴权后的进程身份，避免跨宿主继续操作时重复安装或下载模型。
- 本地模型页面通过经过鉴权的 SSE 接收进度；切换页面或隐藏窗口时暂停观察，连接中断后退避重连。下载字节更新不再反复读取完整模型与隐私状态。

下载适合平台的压缩包；Windows 安装需要同版本 manifest 和 `install-windows.ps1`。校验值见 `SHA256SUMS`，构建与验收范围见 `BUILD-INFO.json`。已有用户按[安装与版本替换说明](../windows-delivery.md)停止原服务后更新程序，保留独立配置、模型与恢复记录。模型权重和推理运行时另行下载。

This preview improves pinned download fallback, macOS memory admission, readable WorkBuddy model selectors, authenticated reuse of an existing installation, and streamed local-model progress. The Wefto executable, storage layout and documented legacy compatibility identifiers remain supported. Download the archive for your platform and verify `SHA256SUMS`; see `BUILD-INFO.json` for the exact source and validation scope.
