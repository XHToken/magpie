# 端云协同 beta.14 草稿验证

此版本仅为 Release 草稿。用户明确选择保留 beta.13 为公开版本；没有发布 beta.14 tag，没有将草稿公开。

候选包和页面检查来自干净的公开源码提交 92d7a03bb7701d11a66746e0669925e745f8d4d3。本文件为后续验证记录，未改变候选包中的 Go 源码、网页资源或打包输入。

- 五个平台候选包均完成构建，归档成员、文件大小、SHA256 和 PE/ELF/Mach-O 架构校验全部通过。
- Windows x64 与 WSL/Linux x64 候选包通过隔离启动、Unicode/空格路径、管理鉴权、嵌入页面、占用端口回避、停止及配置保留检查。
- Linux x64 的重复构建归档、manifest 和 SHA256SUMS 字节一致。
- 产品构建和 27 个产品包的 vet：passed / passed。Chromium：32/32；WebKit：32/32。包括新增补处理页面、五语言及窄屏检查。
- 页面检查是诊断运行，race 未包含在该次 gate 中，整体状态为 partial，不能称完整产品门禁通过。
- 独立原生 Windows race：七个受影响包通过；routerservice.TestHostOnlyCurrentPromptReachesModelAndRestores 的宿主/目标协议分支报 redaction_extraction_failed。单独复核仍失败。
- 应用到本地开发 main 后，八个受影响包的普通测试：全部通过；验证候选为 c1222394b27bbee97e88879240244784a2b086fd。本地 main 保留开发历史，公开 main 保留公开历史。
- 未重跑全仓库 nogui 套件；此前实施记录已有失败，尚未排除。该候选没有完成通过的 GitHub Actions 结果。本次未改动仓库 Actions 设置。

## 仍阻止公开发布的项目

- Deleted user history can leave unprotected original values in assistant/tool echoes; trusted host lifecycle is incomplete.
- Linux/macOS persistent redaction backends are unimplemented; production redaction blocks on those platforms.
- Windows native routerservice race test failed with redaction_extraction_failed.
- Key rotation, partial initialization recovery, backup/restore and record repair tools remain incomplete.

macOS Intel/Apple Silicon 与 Linux ARM64 只完成交叉构建和静态包检查，未进行对应原生启动。未运行真实模型质量、付费云端请求、完整宿主编辑/删除或断电恢复验收；未重跑 Windows 离线安装流程。Windows/macOS 包未签名，macOS 未公证。Linux/macOS 包构建成功不表示新的生产脱敏功能可用。

草稿含五个平台归档、五份 manifest、独立 Windows 安装脚本、Unix 说明、BUILD-INFO.json 和 SHA256SUMS，共 14 个附件。SHA256SUMS 覆盖其他 13 个文件；上传后逐一核对附件大小与 GitHub 返回的 SHA256。公开安装指引保持 beta.13。
