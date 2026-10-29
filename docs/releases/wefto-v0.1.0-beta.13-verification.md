# 端云协同 beta.13 发布验证

本次整合基于公开 beta.12（`321333edbcb88ada77fcd0c72b6a24801e1ccfab`）。产品构建和完整离线门禁验证候选为 `9cde8f91a3e7029f80a64576712c8540e6dd017e`。随后仅补充发布验证记录文档；编译进产品的 Go 源码及网页资源保持一致。

- 完整离线产品门禁通过：27 个包的 race/vet、Chromium 页面套件和 WebKit 页面套件。
- 回归入口 19 项通过。
- 临时 HOME、临时测试目录和固定 Go 缓存中的全仓库 `go test -tags nogui ./...` 通过。
- Linux、Windows、macOS amd64 的全仓库 nogui vet/build 通过。发布产品的五种平台/架构包均构建完成。
- 五个平台归档的成员、架构、长度和 SHA256 校验通过；Linux x64 重复构建字节一致。
- Windows x64 与 WSL/Linux x64 的实际包通过隔离启动、管理鉴权、占用端口回避、正常停止和配置保留检查。

全仓库检查使用 Linux 原生工具 PATH；上游 Magpie 桌面默认 Linux vet/build 因本机缺少 fontconfig、GTK 和 WebKit 开发库而不可执行，独立 Wefto 产品及全仓库 nogui 检查不依赖该桌面图形栈。

macOS Intel/Apple Silicon 与 Linux ARM64 是交叉构建和静态包校验，没有在对应原生系统执行。本次没有重跑真实模型推理、付费云端请求、全部宿主端到端流程或脱敏识别质量验收。Windows/macOS 包未签名，macOS 未公证。以上均为本地验证结果。

准确源码、构建环境及验证范围随 Release 的 `BUILD-INFO.json` 发布；`SHA256SUMS` 覆盖其余 13 个附件。
