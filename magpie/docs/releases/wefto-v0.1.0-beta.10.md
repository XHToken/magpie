# 端云协同 0.1.0-beta.10

本版本整合 beta.9 之后的本地修复，继续沿用 Wefto 命令、数据目录及既有 API-router 兼容规则。

- 嵌套路由组继承脱敏要求，并将要求绑定到单次请求。子组本地模型不可用、云端回退或成员为空时不再丢失隐私策略；编辑预览和请求记录显示继承来源与阻断原因。
- 默认允许 X2.6 Q4_K_M/Q8_0 实验性脱敏，仍需应用或路由组开启脱敏并单独启用模型。新版显式关闭会保留；旧版无版本标记的启动选项采用开启默认值。无效识别结果继续阻断云端请求，识别质量尚未正式验收。
- 供应商模型目录发现与推理协议分开。区分成功发现、刷新失败后的缓存及未经验证的预置列表；晚到的发现结果不能覆盖已修改的连接。
- WorkBuddy 多轮请求保留 step sequence 元数据，避免后续轮次校验失败。
- macOS/Linux 的 Loomy 原生模型配置刷新覆盖现有原生目录，无需关闭应用。
- 本地模型检查失败显示具体结果；重试和进度提示使用一致的阶段信息。
- macOS/Linux 运行时归档中的安全动态库软链接展开为普通文件，支持链接链和前向引用，并继续拒绝越界、循环、缺失或目录目标。Intel macOS 展开预算调整为 64 MiB。
- 安装技能每次需要下载时实时选择最新兼容 Release，并使用该 Release 返回的附件地址。
- 页面日期与数值格式化兼容 Linux 浏览器可能报告的 POSIX 语言标记，避免正常服务被误报为连接失败；有效的英式等地区格式继续保留。

提供 Windows x64、macOS Apple Silicon / Intel、Linux x64 / ARM64 包。Windows 安装需要同版本 manifest 和 `install-windows.ps1`；所有附件校验值见 `SHA256SUMS`，准确源码与验证范围见 `BUILD-INFO.json`。升级前停止旧程序，保留配置、模型和宿主恢复记录，按[版本替换说明](../windows-delivery.md)操作。

This preview preserves inherited request privacy through nested routing groups and cloud fallback, separates provider model discovery from inference protocols, retains WorkBuddy turn metadata, refreshes native Loomy configuration, and materializes safe runtime library aliases on macOS/Linux. Experimental X2.6 Q4/Q8 redaction startup permission defaults on; application/group intent and model enablement remain required. Invalid recognition results still block cloud dispatch. Formal recognition quality and native macOS model loading are not certified. Exact build provenance and verification scope accompany the packages in `BUILD-INFO.json`.
