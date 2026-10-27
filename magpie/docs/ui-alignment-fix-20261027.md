# 管理页面对齐修复（2026-10-27）

## 修复范围

- Agent 模型弹窗：名称、标记和排序按钮使用固定列位；名称按内容换行并撑开行高，长标记省略显示且保留完整悬停提示。Loomy 与 WorkBuddy 共用修复。
- 弹窗标题与数量增加间距；分组标题、折叠箭头及底部主模型说明对齐。折叠隐藏对应模型行，搜索区在窄窗口中可换行，弹窗高度受实际可用视口约束。
- 本地模型状态：标签和状态值使用统一列位；GPU 回退提示放进“推理后端”行，保持与标签相同的左侧缩进，并与下一行分隔。长状态值允许换行。

改动位于 `internal/routerservice/assets/router.css`、`profiles.js` 和
`local-model.js`，仅涉及管理页面展示，不改变模型选择、Provider 或宿主配置协议。

## 验证

`agents.test.cjs` 使用截图中同长度的 Provider ID，以及普通和超长模型名，
检查两个宿主、五种语言、1440px 和 375px 视口下的文本行高、列间重叠、
弹窗边界及折叠行为。`local-model.test.cjs` 在五种语言、1600px 和 375px
视口下检查 GPU 提示位于后端状态行内、左侧对齐、处于状态值下方且不覆盖下一行。

旧项目二进制 `dist/api-router.exe` 运行新增 Agent 检查时失败，报告
`model text outside row` 和 `overlapping columns`，复现了原始问题。
旧二进制的本地模型测试更早被已有文案差异阻断，因此不把该失败算作 GPU 布局复现证据。

测试服务使用独立临时配置、宿主目录和随机回环端口；未修改实际宿主配置。
运行日志与截图保存在 `dist/layout-*.log` 和 `dist/layout-qa/`。

- 完整页面回归构建：`dist/wefto-layout-final.exe`，由 Go 1.26.8 交叉编译。
  SHA-256：`a3b92b67c5e3dded9597ea7a354b944e7a3e5d58ed6758133ad3b047bceeb2e4`。
- 原生 Windows Chromium、WebKit 完整产品页面回归：各 28 项通过，0 失败，覆盖五种语言及桌面/窄窗口。
- 额外检查发现通用 `.fold` 样式会影响折叠分组的标题宽度；新增检查在该构建上报
  `folded headers keep the same columns`。产品弹窗已覆盖该通用样式，确保折叠标题仍逐行对齐。
- 最终构建：`dist/wefto-layout.exe`，SHA-256：
  `c879ba68dc4826e7ac7dfa28453e065d0a451d4b9f9ae7cf2438de723e6dd7e1`。
  补充修正后，包含折叠标题检查的完整 Agent 用例在 Chromium、WebKit 均通过，
  日志为 `dist/layout-verified-chromium.log`、`dist/layout-verified-webkit.log`。
- JavaScript 语法检查及本次文件的 `git diff --check` 通过。
- 本次未运行全仓 Go 测试、竞态测试或多平台 vet；变更仅为管理页面布局。

## 当前运行实例

普通项目入口的只读检查发现，当前配置由安装版
`C:/Users/yzxu11/AppData/Local/Programs/Wefto/wefto.exe` 持有。
使用该安装版自己的 `service status` 已确认身份。本次不接管该实例，
不改它的二进制、配置或宿主地址；项目源码和候选构建的修复需要在使用该构建时生效。
