# X2.6 1.7B 接入复查（2026-11-02）

范围：`spark-x2.6-1.7B`（Q4_K_M）、`spark-x2.6-1.7B-Q8_0`，以及现有两款 4B 的兼容性。

## 结论与修复

发现并修复一项下载恢复缺陷：主源已有断点、随后返回 429 时，下载器进入备用源前就删除了原 `.part` 和元数据；备用源连接失败后，原下载进度丢失。本次真实 Q8 下载在重试前保留约 33.6 MB，随后遇到镜像 429、官方源连接超时，复现了该现象。

`routerdownload.downloadSource` 现在等备用源返回可用完整响应后才截断旧文件。备用请求不携带另一源的 Range/If-Range；连接错误、HTTP 错误、编码异常及意外的 206 响应保留原断点和验证器。后续重试仍须通过完整长度与 SHA256 校验才能发布安装文件。

新增 `TestDownloadFailedMirrorPreservesResumablePrimary`，对四款模型分别覆盖上述四类错误，共 16 个场景，并验证恢复后确实从原偏移继续下载。修复前全部因 `failed fallback discarded resumable primary bytes` 失败；修复后通过。Linux 和 Windows 的下载器完整包测试均通过。

同时修正快速入门 Markdown/HTML、用户指南 HTML 和运行时设计文档中遗留的 X2.5 本地模型名称，统一为 X2.6，并补齐 Q4_K_M / Q8_0 后缀说明。

## 验证范围

| 检查 | 结果与证据 |
| --- | --- |
| 首轮 26 个产品包 build、race、vet | 全部通过；`magpie/dist/regression/spark17-comprehensive-20261102/` |
| 首轮 WSL 浏览器入口 | 未启动：默认缓存包含 Windows 浏览器，缺少所需 Linux 可执行文件；未计为通过，随后改用 Windows 原生浏览器 |
| Windows Chromium 全部 18 份页面测试 | 38/38 通过；`magpie/dist/regression/spark17-native-audit/chromium.log` |
| UI 程序与工作区一致性 | `app.js`、`local-model.js`、`profiles.js`、`provider.js`、`observations.js`、`tutorial.js`、`router.css` 全部逐字节一致；`magpie/dist/spark17-audit-tools/tested-assets.json` |
| Windows WebKit 全部 18 份页面测试 | 38/38 通过；`magpie/dist/regression/spark17-native-audit/webkit.log` |
| Windows 原生 Go 检查 | routermanifest、routerdevice、routerruntime、routerservice、routerdownload 五包全部通过；后者包含修复后的完整包测试 |
| 修复后 26 个产品包 build、race、vet | 全部重新通过；`magpie/dist/regression/spark17-download-fix-20261102/`。该轮只请求后端阶段，入口整体记为 partial；UI 结果取上述独立 Windows 全套，不将未请求阶段算作通过 |
| 普通用户服务的真实页面 | 1440/375/320 像素检查通过；两个 1.7B 选项可预览、下载按钮可用、已运行的 4B 开关保持开启；无页面脚本错误、横向溢出或模型/应用/宿主配置变更 |
| 五种语言及窄屏跨页面专项 | Chromium、WebKit 均通过；包括本地模型列表、入口定位、精确选择、未安装项过滤、Loomy/WorkBuddy 保存/排序/刷新、路由组与请求观察；安装/运行状态采用夹具，宿主文件写入仅在临时目录进行 |
| 官方元数据与模板 | 官方固定提交的两款权重大小和 SHA256 与目录一致；模板由生产下载器完整下载并验证通过 |
| 格式与语法 | 已改 JavaScript/CJS 的 `node --check`、Go 格式和 diff 空白检查通过 |

截图实际复查了普通用户服务的窄屏模型页，以及隔离测试中的 WorkBuddy 320 像素选择器、四款模型安装列表和路由组卡片。长名称换行后仍能区分模型档位与量化。

## 真实推理仍未验收

两款新增模型均未取得完整、校验通过的权重。镜像源返回 429，WSL 官方源连接超时；Windows 官方源下载也出现短读或低速，限定时间内未完成。另一次官方 CDN 分段尝试因持续无有效进度停止，保留部分文件，未改用未登记来源。

因此，本次不能确认新增模型的真实 JSON/SSE 对话、连续多轮、工具调用、64K 加载、判断/执行能力、脱敏质量及响应性能已通过。目录、模拟生命周期或前端测试通过均不替代这些验收。真实 Loomy/WorkBuddy 应用中的完整对话亦未验证。macOS 真机推理不在本次证据范围内。

普通用户服务保持原 4B 模型及宿主设置；源码修复尚未替换常驻 EXE。修复验证构建和原始失败日志保留在 `magpie/dist/spark17-audit-tools/`，真实页面截图及摘要保留在 `magpie/dist/spark17-live-ui/`。下载残片均在隔离的 `magpie/dist/spark17-*weights/` 或 `spark17-native-ranges/`，没有写入普通用户模型安装目录。

Windows 修复验证构建：`magpie/dist/spark17-audit-tools/api-router-fixed.exe`，SHA256 为 `14086eb59ffdc25fd018aaa19490a9061b0a8dc62aed02b7be37c88e9f1ba9f3`。

全部检查结束，完整摘要为 `magpie/dist/spark17-audit-tools/validation.json`。普通服务最终复核仍为 `http://127.0.0.1:13145/`、服务 PID 12328、原 4B 模型 PID 39828、`healthy: true`、`runtime: idle`；没有替换常驻程序、切换模型或改写宿主设置。隔离页面服务、浏览器和下载进程已退出，回归锁已释放。
