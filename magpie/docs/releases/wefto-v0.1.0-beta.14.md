# 端云协同 0.1.0-beta.14（预发布）

本次预发布已补齐 Linux/macOS 的持久脱敏系统密钥后端。五个平台包均由包含新实现的源码重新构建。

- Linux 接入用户会话的 Secret Service，macOS 直接接入系统 Keychain；Windows 保留 DPAPI。磁盘只保存密钥引用或受保护的密钥包，没有明文密钥兜底。
- 补齐 Unix 私有权限、属主/链接检查和排他锁。已完成消息的历史识别结果继续复用，未知历史走显式补处理，响应按请求快照还原。
- Linux 在隔离的真实 GNOME Keyring 下完成 HTTP 出站保护、响应还原、跨进程重开和锁定保护验证。Windows/WSL 脱敏和服务包 race 全部通过。
- Windows 全仓库 vet、Linux/macOS 全仓库 nogui vet 和三平台产品构建通过。macOS Intel/Apple Silicon 仅完成交叉检查，新增临时 Keychain 原生测试尚未在 Mac 执行。

Linux 需要可用且已解锁的用户会话 Secret Service；macOS 需要已解锁的默认 Keychain。不可用时明确阻断，解锁后重启服务重试。

本版本按预发布公开，仍有以下已知限制：可信宿主编辑/删除生命周期和删除原文后的旧回显保护、完整恢复管理尚未完成；macOS 原生验收尚缺。完整仓库测试中未修改的 agent 包出现超时，结果见验证记录和 BUILD-INFO.json。GitHub Actions 被仓库禁用，无法运行 macOS 原生 CI。此次公开发布不代表完整验收通过。

附件包含五个平台归档、五份 manifest、安装脚本、Unix 说明、BUILD-INFO.json 和 SHA256SUMS，共 14 项。
