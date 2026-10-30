# 端云协同 beta.14 Unix 后端发布验证

本次预发布包已由 `80965ca0f712395503e26e06658fdd56a05fe04c` 重新构建，该提交及其打包输入为干净状态。后续发布记录仅更新本文件和 Release 说明，不改变 Go 源码、页面、打包脚本或包内用户说明。

## 已通过

- Windows 原生、WSL/Linux 的 `routerredact` 与 `routerservice` 完整包 race 检查全部通过。
- Windows 全仓库 vet、Linux/macOS 全仓库 nogui vet 通过；Windows、Linux、macOS 产品构建通过。
- macOS Intel/Apple Silicon 的测试程序交叉编译通过，两个架构的受影响包 vet 通过。
- Linux 使用隔离的真实 GNOME Keyring / 私有 D-Bus，通过实际 HTTP 脱敏出站、JSON 还原、第二轮历史复用、新进程读取和密钥锁定后保留数据库检查。
- 原先只在 Windows 编译的历史测试现为共享测试，覆盖重启、零命中复用、显式准备、并发合并、密文篡改和跨应用隔离。
- 新增 Unix 路径/锁回归在旧实现上真实失败，错误为 `redaction_store_platform_unsupported`；修复后通过。没有降低生产规则的超时或放行保护。
- 五个平台候选包均通过归档成员、权限、大小、SHA256 及 PE/ELF/Mach-O 架构检查。Windows x64 和 WSL/Linux x64 包通过隔离启动、管理鉴权、嵌入页面、占用端口回避、正常停止及设置保留检查。
- gofmt 和 git diff --check 通过。本次没有页面资源修改，未重复运行浏览器套件；先前候选的 Chromium/WebKit 各 32 项通过不能替代本次原生后端检查。

## 未通过及未完成

- 全仓库 `go test -tags nogui -p 2 ./...` 中，未修改的 `internal/agent` 在默认 10 分钟时限后超时；CLI/同步测试仍未结束，本轮随后停止并清理剩余测试进程。因此全仓库结果为失败且未完成，不宣称完整门禁通过。未作独立基线归因。
- 本机没有 macOS 原生运行环境。已实现直接调用 Security.framework 的 Keychain 后端，已添加使用临时 Keychain 的 `TestMacNativeKeychainRoundTripAndLock`，但尚未在 Mac 执行。交叉编译通过不是 Mac 原生验收。
- macOS Intel/Apple Silicon 和 Linux ARM64 包仅完成交叉构建及静态校验。未验证真实模型识别质量、付费云端请求、完整宿主编辑/删除、断电恢复或 Windows 离线安装流程。
- 可信宿主编辑/删除生命周期、源历史被删除后的旧回显保护、密钥轮换及完整备份恢复仍是发布限制。Linux 无桌面环境时需要配置可用且已解锁的 Secret Service；macOS 默认 Keychain 也必须可用且已解锁。

Windows/macOS 包未签名，macOS 未公证。GitHub Actions 已被仓库禁用，原生 CI dispatch 返回 HTTP 422，不能把交叉检查当作 Mac 原生验收。

用户在知悉以上验证范围与已知限制后要求公开发布 beta.14。本版本以预发布形式公开，标签为 `wefto-v0.1.0-beta.14`，指向包含本记录的公开 main 提交。上述缺口继续保留，不宣称完整验收通过。准确源码、构建配置和实际检查见附件 BUILD-INFO.json，SHA256SUMS 覆盖其他 13 个附件。发布前后均核对全部 14 个附件的大小与 GitHub SHA256。

本地日志与机器可读结果：`D:/api-router/.git/cross-platform-redaction-oct30/`；重构建包及静态验证：`D:/api-router/.git/unix-backend-beta14/`。
