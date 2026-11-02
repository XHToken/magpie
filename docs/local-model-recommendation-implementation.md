# 端侧模型推荐策略本地实施记录

以下为早期 API-router 工作目录的本地验证记录。beta.6 发布整合以最新 Wefto 主线为基础，保留正式 Wefto 命令、目录兼容与宿主协议；最终发布候选的验证以 PR/CI 和 Release 构建记录为准。

当前策略：`spark26-64k-v3`。实施依据：[已批准标准](local-model-recommendation-proposal.md)。

## 2026-11-02：新增 1.7B Q4_K_M / Q8_0

- 目录增加 `spark-x2.6-1.7B` 和 `spark-x2.6-1.7B-Q8_0`，展示名分别带 Q4_K_M、Q8_0；原有 4B 模型身份、权重、模板和安装记录保持稳定。
- 两款 1.7B 使用官方 GGUF 固定提交 `3c90a598d4e4a030703080b5f278fe098247fbef`，模板固定提交 `e6406af83988f26e87ea92455f31db3f6476131b`。SHA256 和实际字节数写入编译目录。HF-Mirror 优先，Hugging Face 官方源回退，两者均使用同一版本和哈希；本次 ModelScope 元数据查询失败，未将其加入新增模型来源。
- 1.7B 自动推荐为 8 GiB 总内存、5 GiB 可用内存、4 逻辑线程及兼容后端；手动试用为 4 GiB 可用内存。两款采用相同预算，1.7B Q8 不继承 4B Q8 的独显条件。4B 既有准入保持不变。
- 先复用健康运行的模型，再按 4B Q8、4B Q4、1.7B Q8、1.7B Q4 的顺序推荐，各自独立检查磁盘预算。检测摘要返回实际推荐的 1.7B/4B 档位。
- 四款共用现有引擎、64K 上下文、断点下载、校验、加载、能力和性能检查。新增模型的实验性脱敏仍使用固定模型白名单及原有能力准入，不把目录元数据视为质量验收。
- 页面提供四款选择，手动试用提示适用于实际所选模型；同步更新五种语言和教学中的模型范围。

### 本次验证

- 新增目录测试先在原实现上失败，提示 `1.7B model spark-x2.6-1.7B is missing from the downloadable catalog`；接入后通过。包含推荐顺序、8/5/4 GiB 边界、4 线程、独立磁盘预算、未知数据和健康模型复用的测试通过。
- 26 个产品包/命令的构建、race 和 vet 通过，记录在 `magpie/dist/regression/spark17-20261102/`。新增两款模型均通过模拟安装、选择、启用、加载和能力准入测试。
- Chromium 全套初次最终检查为 30/32；修复两个测试继承 WSL `en-US@posix` 语言的问题后，两项专项均通过。WebKit 全套为 31/32；本地模型测试曾将前一次 Q4 选择的响应当成 Q8 响应，改为等待操作完成并匹配准确目标后，在 Chromium、WebKit 均通过。其余用例已通过，未重复执行。所有页面验证使用同一 SHA256 的产品二进制 `5e251cb21a719341cafcd2bc387c2e0a82d935025d5c663e49ff6e17f088e13d`；旧的失败记录保持原样，不宣称单次完整入口全绿。
- 同时修复了已有测试的旧入口与断言：历史准备页使用本轮产品构建；WorkBuddy 检查已保存的可读别名及其精确路由映射；应用卡片等待所需应用而非固定总数；无模型时脱敏可用性允许启动中的 unknown/unavailable，仍断言未启用识别。
- Windows 原生运行 routermanifest、routerdevice、routerruntime、routerservice 四包相关专项通过；Windows x64 和 macOS ARM64 的产品构建、26 包 vet 与 Windows 四包测试交叉编译通过。此处不代表 macOS 真机推理。
- 两个权重和模板在 HF-Mirror、Hugging Face 的六个 Range 探测均返回 206、正确总长度和非零字节；官方模板完整下载后的 SHA256 与目录一致。完整权重未下载，尚无新增模型实机推理或质量验收结论。
- 汇总为 `magpie/dist/regression/spark17-validation.json`。更改保留在源码工作区，Windows 构建位于隔离验证目录；未部署常驻服务。JavaScript 语法、Go 格式与 diff 空白检查通过。

### 前端整体复查

- 本地模型下载选项、已安装列表、供应商入口、Loomy/WorkBuddy 模型选择和主模型、路由组成员/规则、教学、请求记录均检查 1.7B Q4_K_M / Q8_0；4B 两款继续保留。未安装模型只在下载目录出现，已有但暂时未安装的保存项保留名称和未验证提示。
- 修复供应商入口进入本地页时未定位到所点模型的问题。入口只改变当前展示项，不切换或关闭正在运行的模型；初次目录加载、重复导航和刷新均覆盖。
- 供应商本地列表以明确的 `installed: true` 为准，已安装但关闭的模型显示“已安装”；只有当前选中、启用且处于可用状态的模型显示“可用”，包含 idle/busy。
- 已安装/供应商列表在窄屏自动换行，完整显示档位和量化后缀；应用状态、主模型提示及悬浮标题中的本地模型使用目录名称，保存的 selector 和宿主映射仍为准确模型身份。云端/路由组详细状态和悬浮标题保留完整 selector，防止不同供应商的同名模型混淆。
- 修复路由组较多时顶部操作栏被压缩、说明文字遮挡“新建路由组”按钮的问题；新增断言检查按钮与说明文字不重叠。
- 修复浏览器继承 POSIX 或非法 locale 时，日期格式化异常导致健康服务被误报断连的问题；请求统计中的日期、数字使用同一安全格式化入口。
- `local-model-surfaces.test.cjs` 专项覆盖五种语言、1440/375/320 像素、两款新增模型逐一切换、真实配置保存/刷新/排序、隔离 Loomy/WorkBuddy 配置写入。Chromium、WebKit 均 6/6 通过，日志和截图位于 `magpie/dist/regression/spark17-surfaces-qa4/`。开发中复现失败的记录保留在 `spark17-surfaces-before`、`check`、`qa2`、`qa3`，未计为通过。
- 已扩展应用保存项、缺失安装提示、路由组规则目标和请求记录测试，检查新增 1.7B 模型身份。浏览器运行时安装/加载状态使用固定投影，不代表实际权重推理验收。
- 补齐网关页面包含 1.7B / 4B 范围说明的德语翻译，避免回退为中文。
- 集成首轮 `magpie/dist/regression/spark17-ui-audit-full/` 的 26 包 build、race、vet 通过；Chromium 36/38，通过后才会执行的 WebKit 尚未运行。两个失败分别定位为云端标题被缩短的兼容问题，以及扩展规则测试时漏建对应规则的夹具问题；前者已恢复完整云端 selector，后者为每个外部目标建立独立规则并断言准确身份。该轮使用 `c9ba6ca34ce73a5816ad02ff1ce8b1ad74ddcaa3294df34e94259c52bd9e2460` 构建，保留失败记录，不计为最终全量通过。
- 修正后重新构建，`magpie/dist/regression/spark17-ui-audit-final-v2/` 中全部 18 份前端测试在 Chromium、WebKit 分别 38/38 通过；两种引擎额外验证了德语网关页 320 像素无横向溢出、说明完整翻译，并保存截图。复查本地列表、应用选择器、路由组和德语网关截图通过。
- 最终 Linux 产品构建 SHA256 为 `e28a286a6b9c0f4e7c065c563210c7d5a0291c55b003e274bed2c9d17528f05c`，Windows x64 构建 SHA256 为 `ec31b0c8da7133956aa107a0345eb18821ee566106475d38e634803d28dd09af`。首轮后仅修改前端和测试，后端 Go 逻辑未再变更；26 包 race/vet 证据沿用本轮集成记录。
- 汇总为 `magpie/dist/regression/spark17-ui-audit-validation.json`。最终 JavaScript 语法、diff 空白检查通过，隔离服务和浏览器均已退出，回归锁正常释放。代码保留在 main 工作区，未替换普通用户的常驻服务或宿主配置，未下载真实权重或进行实机推理验收。

### 用户测试启动（2026-11-02）

- 用户要求启动最新项目后，正式脚本首先复用普通用户安装的 Wefto 0.1.0-beta.14；实际目录仍只有 4B 两款。随后使用该实例自己的认证 CLI 正常停止服务，将本轮已验证的 Windows 构建复制到项目正式入口 `magpie/dist/api-router.exe`，通过 `scripts/start-project.ps1 -UseProjectBuild -NoBrowser` 启动。
- 原项目 EXE 备份在 `magpie/dist/startup-backups/spark17-20261102-135835/`，同时保留配置/凭据和 WorkBuddy 文件切换前后的哈希记录；四份文件哈希均未改变。安装目录中的旧 Wefto EXE 保留，未改动登录自启设置。
- 启动后以 `-CheckOnly -NoBrowser` 复核，实际地址为 `http://127.0.0.1:13145/`，PID 12328，普通用户配置和模型目录仍为 `api-router`。在线目录返回全部四款模型，管理页面 HTTP 200，前端包含入口定位修复。原 selected/enabled 意图保留为 4B Q4，新模型尚未自动下载。上述地址和 PID 为本次启动记录。

以下为 2026-10-27 的原有 4B 实施与验证记录，其服务 PID、端口及构建仅为当时记录。

## 可见行为

- 当时产品目录为 Spark-X2.6-4B-Q4_K_M 与 Spark-X2.6-4B-Q8_0，使用 65536-token 上下文。
- 后端按准确模型 ID 分别给出自动推荐、手动安全准入、资源问题与各目标卷空间预算。页面和 api-router-start skill 消费同一份 `device.advice`。
- 已知硬件偏弱返回 `not_recommended`，临时资源不足、关键事实未知返回 `deferred`。检测不下载、不启用、不卸载模型，也不持久化默认推荐。
- GPU 使用单卡当前空闲专用显存扣除保留量；CPU 大内存设备默认 Q4。已加载健康模型优先复用，避免重复计算已占内存。
- 安装和每次冷启动重新准入；GPU 失败回退 CPU 时重新检查 CPU 预算。逐模型、逐卷空间计划仅抵扣已验证缓存，并对共享组件去重。
- 原有能力检查后执行两次短合成 SSE 性能采样。性能慢或待确认时保留模型和用户意图，阻止新的自动协同使用。性能证据绑定模型、运行时、PID 和进程代次，重启、崩溃及 PID 复用不能继承旧速度评级。
- 简中、繁中、英文、日文、德文页面展示资源不足的数值、手动试用提示与性能状态。

## 已完成的针对性验证

- routerdevice/routerruntime/routerprocess/routerservice 四包测试通过。
- 最终策略与运行时全部测试 `-race -count=20` 通过；服务推荐、采样和回退边界另有 20 次竞态验证。
- 使用 Go overlay 临时移除 16 GiB 自动推荐门槛，`TestModelAdviceQuantizationAndResourceBoundaries/below16GB_cloud` 按预期失败；正式源码未被替换。
- Windows 原生测试覆盖模型建议、体验状态、缓存计划、冷加载拒绝与 GPU 回退，全部通过。
- Windows 与 macOS arm64 的受影响四包 vet 通过；无 macOS 真机推理验收。
- Windows 原生 Chromium，以及 WSL Chromium/WebKit 的推荐页测试覆盖五种语言、1600/375 像素宽度，通过。
- skill 两份副本 SHA256 一致，quick_validate 通过；仓库 skill 检查 13 项通过。
- 隔离 Windows 原生服务检测确认了真实卷 GUID，并返回结构化推荐；检测没有下载、加载或改变启用状态。

## 完整集成回归与当前服务

- 集成检查：`magpie/dist/regression/run-5kFEhp/` 的 26 个产品包/命令 build、race、vet 通过，Chromium 全套 28 项通过。此构建生成后补充了总内存说明及更严格的页面断言；WebKit 运行到该新断言时，旧嵌入资源不包含说明，按预期失败。因此该次整套运行不是最终构建的完整通过证据，其余 WebKit 全套未跑完，不宣称全量双引擎通过。
- 最终源码：routerdevice/routerruntime 全部 `-race -count=20` 通过（`magpie/dist/recommendation-evidence/recommendation-final-core-race2.log`），routerservice 全包测试通过，推荐/采样/回退服务边界另经 20 次竞态检查。
- 最终匹配构建：`magpie/dist/recommendation-evidence/recommendation-final-page-checks.log` 中 local-model 与 model-advice 两套页面，在 Chromium、WebKit 分别 2/2 通过，覆盖五种语言与两种宽度；包含上述总内存说明断言。Windows 原生同页及运行时新旧进程/PID 复用测试通过。最终四包 Windows/macOS vet 与 gofmt 检查通过。
- 中途为补充边界检查停止过一轮开发回归及一轮排队的重复浏览器回归，均未作为成功证据。
- 已部署正常目录 `magpie/dist/api-router.exe`，版本 `dev+model-advice-20261027`。二进制 SHA256：`F78FAB6E4FA31AD124E1DBEB4F2562F92BD8695F4162BDCD27551BEAC28FBB7F`。
- 通过项目正式启动脚本启动并复查：实际管理地址 `http://127.0.0.1:13145/`，PID 15252。原 EXE 留有可恢复备份；配置与凭据文件 SHA256 在切换前后完全一致。
- 正常用户目录的只读检测：32 GiB / 14 线程，当前最低可用约 6.83 GiB，返回 `deferred / available_memory_insufficient`。Q4 已安装并校验，原 selected/enabled 意图保留，因当前资源不足保持 unloaded；未自动下载 Q8，检测前后 intent/PID 不变。该数值是回归期间的瞬时状态，用户释放资源后应重新检测。
- 浏览器测试进程结束后再检测，可用内存约 7.29 GiB，仍为 `deferred / available_memory_insufficient`。测试子进程已回收，确认所有权后已释放本次回归锁。
- 两款目录与 65536 上下文已在实际服务读回。此修改没有额外下载权重或调用真实模型推理；既有 Q4 缓存按正常安装校验路径保留。

## 验证边界

未为此次修改下载两款实际权重，未运行真实权重的 64K 推理与速度校准，也未调用收费云端接口。资源与体验门槛是已批准的保守初值，实际推理可用性、长上下文峰值和业务质量仍需独立实机验收。
