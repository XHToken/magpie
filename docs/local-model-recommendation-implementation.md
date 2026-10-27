# 端侧模型推荐策略本地实施记录

以下为早期 API-router 工作目录的本地验证记录。beta.6 发布整合以最新 Wefto 主线为基础，保留正式 Wefto 命令、目录兼容与宿主协议；最终发布候选的验证以 PR/CI 和 Release 构建记录为准。

策略：`spark26-64k-v1`。实施依据：[已批准标准](local-model-recommendation-proposal.md)。

## 可见行为

- 产品目录只有 Spark-X2.6-4B-Q4_K_M 与 Spark-X2.6-4B-Q8_0，仍使用 65536-token 上下文。
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
