# 端云协同 0.1.0-beta.12

- 应用和路由组的本地模型选择器只提供已安装模型。已安装但未启用的模型仍可选择；已经保存但安装缺失的应用选择保留并标明尚未验证。
- 请求记录筛选标签点击后立即生效，支持再次点击清除和键盘操作。
- 移除本地模型页面的脱敏诊断区域及对应状态轮询，应用和路由组的脱敏设置继续保留。
- 路由组编辑器进一步移除固定的“模型列表”展示行，保存时继续隐藏成员模型的单独条目。
- 整合 macOS 后台服务进程识别、目录和系统临时路径兼容、可用内存与运行时依赖检查，并增加 macOS/Linux 源码启动入口。
- 增加项目架构说明图；延续 Wefto 命令、目录及已安装版本兼容规则。

提供 Windows x64、macOS Apple Silicon / Intel、Linux x64 / ARM64 包。附件来源和实际验证范围记录在 `BUILD-INFO.json`，校验值见 `SHA256SUMS`。macOS 和 Linux ARM64 包需要对应平台的原生运行验证；本次不宣称完成真实模型推理或宿主端到端验收。

This preview filters local-model pickers by installed state, applies request filter chips immediately, removes local redaction diagnostics and the fixed routing-group model-list row, and integrates macOS lifecycle/path compatibility and a Unix source startup entry. Existing selections, routing behavior and installation compatibility are preserved. See BUILD-INFO.json for exact source provenance and validation scope.
