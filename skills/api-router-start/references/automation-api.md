# 自动配置的接口线索与版本检查

仅在一键配置需要时读取。不是跨版本固定 API 契约。先用安装程序的 `version`/帮助和实际页面请求确认该版本能力，源码仓库的另一分支不能作为直接写入当前实例的依据。界面已有一键动作时可用界面；没有时可组合下列受支持操作。始终使用已验证实例的回环 URL、同源管理认证，不向上游发管理 token，不输出凭证。写入不跟随重定向。

当前项目源码中的流程线索：

| 功能 | 接口与注意事项 |
| --- | --- |
| 本地状态与 catalog | `GET /api/local-model`，读取 `status`、`catalog`、`models`；从目录取准确 `id`、`quantization`、安装预留量，从候选取对外选择器；不能从展示名拼 ID |
| 设备推荐 | `POST /api/local-model/detect`，空 JSON；响应 `device` 是 report；以 `device.advice` 的 state/modelID/quantization/reuse/assessments 为准，旧 recommendation/detection 仅是兼容摘要；按 [一键配置](one-click.md) 判断自动推荐与手动准入 |
| 选择 | `POST /api/local-model/select`，`{"model":"<catalog ID>"}`；活动模型切换须按该版本生命周期执行 |
| 安装 | `POST /api/local-model/install`，可用 `{"model":"<catalog ID>"}` 指定安装；异步任务需读状态，不因 200 就认定完成 |
| 启用/加载/健康/检查 | `/api/local-model/enable`、`load`、`health`、`check`，各自 POST 空 JSON；enable 的准确前置条件以版本为准，若安装 UI 在安装前设置 intent 则沿用该流程，最终安装校验后再确认 enabled/loaded；check 包含性能采样，读取 status.performance，acceptable 才自动开启端云协同 |
| 必要的判断/执行试用 | `POST /api/local-model/trial/enable`，已通过证据后按版本使用 `{"judgment":true,"execution":true}`；不调用脱敏配置 |
| 供应商/模型 | `GET /api/cloud-providers` 只取安全字段；保存/模型目录/生成检查的具体 schema 以当前版本页面和 provider 文档为准，勿发明通用 check URL |
| 路由组 | `GET/POST /api/routing-groups`，策略字段 `routing: "smart_split"`，`members` 用实际选择器；写入前读取原值及可用目标，保存后读回 |
| 宿主检测/事务 | `GET/POST /api/hosts/auto`；当前实现 POST 有 `host`、`enabled`、`models`、`configRoot` 等字段，使用实际版本 schema；按应用分别提交、保留原选项与恢复记录 |

在已确认的 schema 下，新组意图示意如下，示例中的值必须由实际 API 返回并代入，不能直接提交占位符：

```json
{
  "id": "<可用且不冲突的组ID>",
  "name": "端云协同",
  "routing": "smart_split",
  "members": ["<已安装本地模型选择器>", "<providerID>/<云端modelID>"],
  "enabled": true,
  "localRedaction": false
}
```

模型安装顺序不靠这个表猜测：参考同版本的 `local-model.js` 和 `local_models.go`。硬件推荐参考 `routerdevice`，能力可用参考 `routerruntime` 实际证据，组和宿主参考 `groups.go`、`host_auto.go` 及对应设计文档。字段、路径、返回结果不符时停止相关写入并诊断；不要为适配脚本直接编辑 config.json/credentials.json/宿主 JSON。

进度只保留非敏感 ID、状态和下一步。供应商表单让用户填写 Key 后保存，自动化从状态读到已保存再继续，不提取 Key 到聊天或命令日志。
