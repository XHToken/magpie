# 本地请求模型显示“未知”的修复与验证

## 问题与修复

Loomy 的标题请求先以普通业务生成建立请求记录，读取请求体后将
purpose 标为 `chat.title`。本地调用生产端原先将所有非 `generation`
目的都标成检查事件并清空业务请求 ID。请求观察器拒收这些事件，
导致本地执行成功且有 Token 用量，但 `localModel`、`localCallId`
和调用明细缺失，页面显示“未知”。

`businessModelRequest` 统一将 `generation` 和 `chat.title` 归为业务请求。
直接调用、端云分流、路由组及云端前本地脱敏均使用同一分类，标题保留
原有业务请求 ID、实际运行模型和用量，同时使用业务请求预算和云端预留时间。
显式管理连接检查仍为独立诊断记录。已有历史记录不使用当前模型选择回填。

## 已完成验证

- 修复前，`TestLoomyLocalTitlesRetainActualModel` 在 1.7B/4B 的直接调用、
  端云分流、路由组路径均复现 `actual local model missing`；普通生成正常。
- 修复后，同一回归测试覆盖普通生成、显式标题标记、原生标题签名及流式
  响应，检查实际模型、调用明细、用量、零云端尝试和诊断检查隔离。
- `TestLoomyTitleRetainsBusinessBudgetAndCloudReserve` 验证总预算、云端
  预留时间及更短显式预算。
- WSL Go 1.26.8：`routerservice`、`routerobserve`、`routerledger`、
  `routerruntime`、`routerrequest`、`routerconfig` 完整包测试通过。
- WSL：`routerservice`、`routerobserve`、`routerledger` 的竞态测试通过。
- Windows：交叉编译的 `routerservice.test.exe` 原生执行整包测试通过；
  单独运行新增回归测试也通过。
- Linux `go vet -tags nogui ./...`、Windows `GOOS=windows go vet ./...` 通过。
- Windows 及 Linux nogui 的 API-router 与根程序构建通过；格式与 diff 检查通过。

## 当前实例

项目程序已替换并通过正常启动入口启动，管理地址为
`http://127.0.0.1:13145/`，版本
`dev+1e2bf47e-dirty.20261027-local-model-log`。更新前后应用绑定一致。
Windows 停止命令返回后会短暂保留进程文件占用；替换程序前已等待所持进程
句柄确认退出，未强制结束端口占用者。

实机使用 Loomy 已绑定的本地模型执行一次标题请求，记录 ID
`ab71f933b184de0373fc86f9a02a79d4`：成功、本地执行，模型
`spark-x2.5-1.7B`，输入 137、输出 2、总计 139 Token，云端尝试 0，
本地调用明细 1 条，`localCallId` 非空。

## 额外全仓库验证限制

`go test -tags nogui ./...` 已运行，但通用 `internal/agent` 和
`internal/cli` 包各累计超过默认 10 分钟时限，不能报告全仓库通过。
超时栈涉及 `provider.GrokExecutable` 对 PATH 候选文件的 `os.Stat`；
当时正在运行 `TestClaudeModelCapabilities` 和 `TestGroupAddSetRemove`。
已停止该全仓库运行及其子进程，另以不含 Windows 挂载路径的 Linux PATH
复核 Agent 套件；复核在 150.554 秒结束，未再触发套件超时。

### 待处理的独立验证问题：Agent 的 Grok 更新测试

上述复核仍有 `TestGrokInstalled` 失败，位置为
`internal/agent/cliupdate_test.go:486`。测试中的临时 Grok 可执行文件更新后，
`grok --version` 返回 `Invalid argument`，不能报告 Agent 套件通过。
复现命令为 `go test -tags nogui ./internal/agent -count=1 -timeout=3m`，
环境为 WSL、Go 1.26.8、临时 HOME、固定 Go 缓存及仅 Linux 的 PATH。
这个失败与本次模型记录问题分别记录，尚未确定其具体原因；本次未修改
通用 Agent/CLI 或 Grok 更新实现，也未声称已经证明它在基线上的行为。

无 `nogui` 的 Linux 静态检查因 WSL 缺少 GTK4、WebKitGTK、GLib、
libsoup 和 fontconfig 开发包而无法完成。
