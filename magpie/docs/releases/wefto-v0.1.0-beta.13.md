# 端云协同 0.1.0-beta.13

- 脱敏控制统一放在路由组策略中，识别多轮对话里的当前用户原始输入，并复用可信映射。
- 识别 WorkBuddy、Loomy 等宿主注入的提示结构，保留原始输入含义和 token 语义；标题请求复用当前输入的脱敏上下文。
- 请求记录显示真正开始推理的本地模型，准确保留本地判断后回退云端的 X2.6 模型信息。
- 模型选择器统一显示目录名称；已安装或运行中的模型不会再次触发安装。
- 验证 Loomy 恢复记录的重连与事务回滚；Windows 启动检查通过可读模型别名找到 WorkBuddy 的实际端点。

提供 Windows x64、macOS Apple Silicon / Intel、Linux x64 / ARM64 五个平台的包，以及安装脚本、清单和 SHA256 校验文件。准确源码与验证结果见 `BUILD-INFO.json` 和[发布验证记录](https://github.com/XHToken/magpie/blob/wefto-v0.1.0-beta.13/docs/releases/wefto-v0.1.0-beta.13-verification.md)。

GitHub Actions 已被仓库禁用，本次没有云端 CI 结果。验证在本地 Windows 与 WSL/Linux 执行；macOS 和 Linux ARM64 仅完成交叉构建与静态包校验。未重跑真实模型推理、付费云端请求或全部宿主端到端流程。Windows/macOS 包未签名，macOS 未公证。

This preview consolidates routing-group redaction, recognizes current user prompts in host envelopes, reuses trusted mappings, records local models that actually began inference, prevents repeated installation of installed/running models, and verifies restored Loomy recovery and WorkBuddy endpoint aliases. Five platform packages are included. GitHub Actions is disabled for this repository; see BUILD-INFO.json for local verification results and platform limits.
