# 端云协同 0.1.0-beta.11

路由组编辑器的“模型列表”固定显示“只列出本组”，移除“也列出成员模型”切换项。

- 新建、固定自动同名组或编辑保存时，统一隐藏成员模型在 Agent 模型列表中的单独条目。
- 已有路由组在编辑保存后采用此设置。组内模型仍按原有路由策略参与请求；明确指定成员模型名的调用仍可使用。
- 同步更新模型列表说明与使用指南。继续沿用 Wefto 命令、数据目录和既有安装兼容规则。

提供 Windows x64、macOS Apple Silicon / Intel、Linux x64 / ARM64 包。Windows 安装需要同版本 manifest 和 `install-windows.ps1`。附件哈希见 `SHA256SUMS`，源码与验证范围见 `BUILD-INFO.json`。升级按[版本替换说明](../windows-delivery.md)操作。

The routing-group editor now displays a fixed “Group only” value. Creating, pinning or saving a group hides its individual members from agent model pickers. Existing groups adopt the setting when saved. Routing strategies and explicitly named member calls keep their existing behavior. Wefto commands, data directories and installation compatibility are retained.
