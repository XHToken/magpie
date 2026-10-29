# 端云协同 beta.12 发布验证

本次整合基于公开 beta.11。产品构建和完整离线门禁验证候选为 `bb8df124fd817e73780fe967ac65542d68443b02`。随后仅补充子系统说明、macOS 清理测试的平台条件和独立 Unix 准备脚本的兼容/锁保留修复；编译进产品的 Go 源码及网页资源保持一致。全仓库检查包含最终的 macOS 测试条件。

- 完整离线产品门禁通过：27 个包的 race/vet、Chromium 31 项和 WebKit 31 项。
- 回归入口 19 项通过；去掉安装状态过滤时，两处模型选择器的新增断言均按预期失败。
- 临时 HOME、临时测试目录和固定 Go 缓存中的全仓库 `go test -tags nogui ./...` 通过。
- Linux、Windows、macOS amd64 的全仓库 nogui vet/build 通过。发布产品的五种平台/架构包均构建完成。
- Unix 准备脚本 15 项行为测试在 Windows x64 和 WSL/Linux x64 通过。新增未验证锁测试在修复前失败，修复后保留记录原字节、不启动其他进程；旧版 macOS 已认证服务的只读检查继续可用。
- 五个平台归档的成员、架构、长度和 SHA256 校验通过；Linux x64 重复构建字节一致。
- Windows x64 与 WSL/Linux x64 的实际包通过隔离启动、管理鉴权、占用端口回避、正常停止和配置保留检查。
- Windows 离线安装、Unicode/空格路径、重复复用、版本检查、卸载预览/执行、离线恢复工具及配置保留验收通过。

全仓库检查使用 Linux 原生工具 PATH；首次继承 Windows PATH 的检查因反复扫描 Windows 挂载目录而被诊断中断，调整环境后重跑。上游 Magpie 桌面默认 Windows vet/build 通过；默认 Linux 桌面 vet/build 因本机缺少 fontconfig、GTK 和 WebKit 开发库而不可执行，独立 Wefto 产品及全仓库 nogui 检查不依赖该桌面图形栈。

macOS Intel/Apple Silicon 与 Linux ARM64 是交叉构建和静态包校验，没有在对应原生系统执行。本次没有重跑真实模型推理、付费云端请求、全部宿主端到端流程或脱敏识别质量验收。Windows/macOS 包未签名，macOS 未公证。Actions 是否可执行以本次 GitHub dispatch 响应及 `BUILD-INFO.json` 为准；以上均为本地验证结果。

准确源码、构建环境及验证范围随 Release 的 `BUILD-INFO.json` 发布；`SHA256SUMS` 覆盖其余 13 个附件。
