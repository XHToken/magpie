# 端云协同 beta.15 发布验证

五个平台包由干净的公开源码提交 `487c2e69a468f7a45d9ba2c1dc14f1e7fa92241b` 重新构建。该源码在 Wefto 公开主线保留命令、产品目录与宿主兼容标识，并整合开发主线 `5fb291b14c310bd15935ce2aad658deefa609fe6` 的 1.7B 接入和断点修复。后续本验证记录只修改文档，不改变包内构建输入。

产品门禁使用干净提交 `8304df0fcd3c6ef2034e9250bef3ce0ed43e7816`；随后仅修正 Node 回归入口的页面数量契约测试（16 改为 18，并要求新增模型页面套件），该测试重新通过 19/19。打包提交与门禁提交之间只有 `magpie/scripts/tests/product-regression.test.cjs` 的断言变化；Go 源码、Go 测试、内嵌网页、浏览器测试、回归执行器与打包脚本完全相同。三平台全仓库 vet 也在该门禁提交通过。

## 本次通过

- Go 1.26.8 的完整 27 个产品包 build/race/vet 通过；所有阶段属于同一干净候选，产品门禁为 passed / complete: true。
- WSL/Linux Chromium 38/38、WebKit 38/38，包括五语言和 1440/375/320 像素跨页面专项。已在合并后的公开源码重新执行，不把之前开发版的结果当作本次包的页面验证。
- Windows 全仓库 vet、Linux/macOS 全仓库 nogui vet 通过。
- Windows x64、Linux x64/ARM64、macOS Intel/Apple Silicon 五包完成独立构建、成员/权限/大小/SHA256 和 PE/ELF/Mach-O 架构校验。
- Windows x64、WSL/Linux x64 包通过隔离的实际启动、Unicode/空格目录、管理鉴权、内嵌页面、占用端口回避、正常停止和设置保留检查。没有替换普通用户的常驻程序或修改模型/宿主设置。
- 下载断点的 16 场景在修复前真实失败、修复后通过；原开发工作区 Windows 下载器全包、设备/目录/运行时/服务包以及原生浏览器验证详见 [接入复查](../spark17-integration-audit-20261102.md)。这些开发记录与本次公开构建结果分别记录。

## 本次未验证与沿用限制

- 新增 1.7B 完整权重下载未完成，真实多轮对话、工具调用、64K 性能和识别质量仍未验收。
- 仓库 Actions 被禁用，原生 CI 调度返回 422；没有本次通过的云端 CI，也没有 macOS 原生执行结果。
- macOS 两种架构和 Linux ARM64 只有交叉构建及静态包校验；macOS 原生 Keychain 验收仍缺。
- 本次未重跑上游全仓库 nogui 测试；beta.14 记录的 agent 超时和不完整结果没有被此产品包门禁替代。
- 可信宿主编辑/删除、删除原文后的旧回显保护、密钥轮换及完整备份/恢复仍保留 beta.14 限制。
- 未运行付费云端、真实宿主完整对话或 Windows 离线安装验收。Windows/macOS 未签名，macOS 未公证。Linux 脱敏需要可用且已解锁的 Secret Service，macOS 需要可用且已解锁的默认 Keychain。

本版本按用户要求以预发布形式发布。附件 14 项，SHA256SUMS 覆盖其他 13 项；准确构建源码、后续文档提交和验证结果见 BUILD-INFO.json。发布时逐一核对远端附件的名称、大小和 GitHub SHA256。本机原始记录位于 `D:/api-router/.git/release-beta15/`。
