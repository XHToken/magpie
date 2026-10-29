# 端云协同 0.1.0-beta.13

- 收敛脱敏控制入口至路由组策略，移除应用页单独的脱敏开关，统一在路由组配置与观测脱敏行为。
- 精细化识别多轮对话中的用户原始输入并执行本地脱敏，跳过宿主注入的系统提示与工具输出，并在后续轮次复用可信脱敏映射。
- 本地判断模型回退到云端执行时，请求记录准确保留实际参与判断的本地 X2.6 模型信息；合并已启用脱敏模型的重复提示。
- 路由组、模型选择器与宿主接入配置统一使用模型目录的规范显示名称；延续 Wefto 命令、目录及已安装版本兼容规则。

提供 Windows x64、macOS Apple Silicon / Intel、Linux x64 / ARM64 包。附件来源和实际验证范围记录在 `BUILD-INFO.json`，校验值见 `SHA256SUMS`。macOS 和 Linux ARM64 包需要对应平台的原生运行验证；本次不宣称完成真实模型推理或宿主端到端验收。

This preview consolidates redaction controls onto routing groups, scopes redaction to original user input while reusing trusted multi-turn mappings, records the actual local X2.6 judge model on cloud fallback, and uses canonical catalog display names across routing groups and host integrations. Existing routing behavior and installation compatibility are preserved. See BUILD-INFO.json for exact source provenance and validation scope.
