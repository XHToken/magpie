# 端云协同 0.1.0-beta.9

本版本包含 beta.8 之后的本地模型准入、宿主兼容和管理界面更新，继续沿用 Wefto 命令、数据目录及既有 API-router 兼容规则。

- 本地模型安装失败按实际平台阶段、下载来源和原因显示提示，区分超时、中断、HTTP、大小/SHA256 校验、文件占用和解压失败。初次读取、进度推送和直接操作失败使用一致说明；重试仍需用户主动操作。
- 对话模板失败后提示复用经过校验的权重，不再承诺立即完成，也不将所有校验失败都归因为网络损坏。
- 设备信息暂时未知时允许显式尝试下载或加载，已确认的资源不足仍会阻止对应操作。
- macOS 下载检查与推理资源门槛分开；Q4 显式试用门槛与自动推荐分别判断，实际加载和健康检查仍须通过。
- Loomy 在 macOS/Linux 按原生配置目录发现已有安装，继续保留显式目录覆盖和恢复事务。
- WorkBuddy 新版请求元数据贯穿标准化、云端执行和脱敏处理，并保留字段类型、角色及隐私检查。
- 路由组编辑器隐藏匹配模式控件；新组提交空模式列表，编辑已有组保留已保存的模式。

提供 Windows x64、macOS Apple Silicon / Intel、Linux x64 / ARM64 包。Windows 安装需要同版本 manifest 和 `install-windows.ps1`；所有附件校验值见 `SHA256SUMS`，准确源码与验证范围见 `BUILD-INFO.json`。升级前停止旧程序并保留配置、模型和恢复记录，按[版本替换说明](../windows-delivery.md)操作。模型权重和推理运行时不包含在程序包内。

This preview improves staged installation errors and safe retries, explicit local-model admission when observations are unknown, macOS download/inference resource separation, native Loomy discovery, current WorkBuddy request metadata and routing-group editing. Verify the platform archive and manifest against `SHA256SUMS`; `BUILD-INFO.json` records the exact source and validation scope. Native startup acceptance does not certify local inference quality or every host integration.
