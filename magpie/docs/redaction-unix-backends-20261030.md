# Linux/macOS 持久脱敏后端修复

本次实现替换 beta.14 草稿中的 Unix 平台占位逻辑。公开草稿及其已有安装包不会自动包含工作区中的新代码。

Linux 使用用户会话 Secret Service 的默认已解锁密钥库，macOS 使用 Security.framework 的默认已解锁 Keychain；两个后端都将随机主密钥保存在系统密钥库，磁盘只留引用和摘要。Windows 继续使用 DPAPI。Unix 补齐私有目录、属主/权限/链接检查及排他文件锁；数据库的逐消息复用、加密、完整出站检查和 JSON/SSE 还原继续沿用共享实现。

首次密钥库连接失败留下的空目录现在可以重试。已有密钥/数据库、损坏内容、缺失密钥或错误权限仍保留现场并阻断。Linux 无图形会话时必须提供 Secret Service；锁定或不可用时不会改用明文文件。解锁后重启服务以重试。

原先只在 Windows 编译的历史测试已改为共享测试。普通测试使用明确标记的合成密钥保护器，不连接真实登录密钥库。原生测试另行验证真实平台 API：Linux 使用独立 D-Bus/临时 GNOME Keyring，macOS 使用临时 Keychain。测试覆盖新进程重开、历史复用、响应还原、真实 HTTP 出站无敏感原值，以及锁定后保留数据库。

首次新增 Unix 权限/锁回归在旧代码上真实失败：`supported Unix state path rejected: redaction_store_platform_unsupported`，修复后通过。此前宿主当前输入测试的大段历史如今需要真实补处理；该测试的历史夹具缩短为 32 次重复，继续验证当前模型只接收新增输入。大历史分批与预算仍由独立历史测试验证，生产超时规则没有放宽。

验证结果与命令保存在 `D:/api-router/.git/cross-platform-redaction-oct30/`。Linux 已完成真实 GNOME Keyring、新进程读取、锁定保护和 HTTP 脱敏链路。macOS 当前只完成 Intel/Apple Silicon 的交叉编译与 vet，本机没有 macOS 原生执行环境；已添加 `TestMacNativeKeychainRoundTripAndLock`，不能将交叉检查称为 Mac 原生验收完成。

Windows 和 WSL/Linux 的 `routerredact`、`routerservice` 完整包 race 均通过；Windows 全仓库 vet、Linux/macOS 全仓库 nogui vet 以及三个平台的产品构建均通过。完整 nogui 仓库测试另行运行并记录，不能以这些定向检查替代全部仓库验收。没有改动网页资源，本次未重复浏览器套件。

本次不解决可信宿主编辑/删除生命周期、历史原文被删除后的旧回显、密钥轮换和完整备份恢复等其他已记录缺口。
