# 端云协同 beta.13 发布验证

本次发布基于公开 beta.12（321333edbcb88ada77fcd0c72b6a24801e1ccfab），整合截至本地 d922bb87 的功能修复。完整产品门禁和最终安装包使用候选 3ed8cb53d5b1b8384a5d24d1f81a5019bb504c02，该候选的工作区为干净状态。后续提交仅更新发布说明与本验证文档，编译进产品的 Go 源码、网页资源及打包脚本不变。

- 完整离线产品门禁通过：构建、27 个包的 race/vet、Chromium 31 项和 WebKit 31 项（包括五语言页面检查）。
- 回归入口 19 项通过；Windows 源码启动测试通过，包括可读 WorkBuddy 模型别名与过期端点拒绝。
- 临时 HOME、临时测试目录和固定 Go 缓存中的全仓库 go test -tags nogui ./... 通过。
- Linux、Windows、macOS amd64 的全仓库 nogui vet/build 全部通过；Windows 默认桌面 vet/build 也通过。
- 五种平台/架构的产品包通过归档成员、架构、大小和 SHA256 校验；Linux x64 重复构建的归档、清单和校验文件字节一致。
- Windows x64 与 WSL/Linux x64 的最终包通过隔离启动、管理鉴权、嵌入页面、占用端口回避、正常停止和配置保留检查。
- Windows 离线安装、精确重复复用、Unicode/空格路径、卸载预览/执行、保留恢复程序及配置检查通过。

GitHub Actions 已被该仓库禁用，workflow dispatch 返回 HTTP 422（Actions has been disabled for this repository）。本次没有云端 CI 结果，以上均为本地验证。上游 Magpie 桌面默认 Linux vet/build 因缺少 fontconfig、GTK 和 WebKit 开发库失败；独立 Wefto 产品及全仓库 nogui 检查通过。

macOS Intel/Apple Silicon 与 Linux ARM64 完成交叉构建和静态包校验，没有在对应原生系统执行。没有重跑真实模型推理、付费云端请求、全部宿主端到端流程或脱敏识别质量验收。Windows/macOS 包未签名，macOS 未公证。

准确源码、构建环境及验证范围随 Release 的 BUILD-INFO.json 发布；SHA256SUMS 覆盖其余 13 个附件。发布前逐一核对 GitHub 附件大小与 SHA256 摘要。
