'use strict';
(() => {
  for (const [key, zh, tw, ja, de] of [
    ["Inference backend", "推理后端", "推理後端", "推論バックエンド", "Inferenz-Backend"],
    ["GPU acceleration", "GPU 加速", "GPU 加速", "GPU アクセラレーション", "GPU-Beschleunigung"],
    ["Hybrid CPU and GPU", "CPU/GPU 混合推理", "CPU/GPU 混合推理", "CPU/GPU ハイブリッド推論", "Hybride CPU-/GPU-Inferenz"],
    ["CPU inference", "CPU 推理", "CPU 推理", "CPU 推論", "CPU-Inferenz"],
    ["Acceleration not verified", "加速尚未验证", "加速尚未驗證", "高速化は未検証", "Beschleunigung nicht geprüft"],
    ["GPU unavailable; using CPU", "GPU 不可用，已使用 CPU", "GPU 無法使用，已使用 CPU", "GPU を利用できないため CPU を使用", "GPU nicht verfügbar; CPU wird verwendet"],
    ["GPU components need installation or repair", "GPU 组件需要安装或修复", "GPU 元件需要安裝或修復", "GPU コンポーネントのインストールまたは修復が必要", "GPU-Komponenten müssen installiert oder repariert werden"],
    ["GPU layers: {loaded}/{total}", "GPU 卸载层数：{loaded}/{total}", "GPU 卸載層數：{loaded}/{total}", "GPU レイヤー: {loaded}/{total}", "GPU-Schichten: {loaded}/{total}"],
    ["GPU components could not be installed; CPU is available", "GPU 组件未安装成功，CPU 仍可用", "GPU 元件未安裝成功，CPU 仍可用", "GPU コンポーネントをインストールできませんでした。CPU は利用可能です", "GPU-Komponenten konnten nicht installiert werden; CPU ist verfügbar"],
    ["Local models are not recommended for this computer. You can use cloud models without downloading one.", "这台电脑的资源不适合当前端侧模型，建议使用云端，无需下载本地模型。", "這台電腦的資源不適合目前的端側模型，建議使用雲端，無需下載本機模型。", "この端末ではローカルモデルを推奨しません。ダウンロードせずにクラウドを利用できます。", "Für diesen Computer werden lokale Modelle nicht empfohlen. Sie können Cloud-Modelle ohne Download nutzen."],
    ["Local download is not recommended right now. Check the resource details and detect again.", "当前暂不建议下载本地模型，请检查资源详情后重新检测。", "目前暫不建議下載本機模型，請檢查資源詳情後重新偵測。", "今はダウンロードを推奨しません。リソースを確認して再検出してください。", "Ein lokaler Download wird momentan nicht empfohlen. Prüfen Sie die Ressourcen und wiederholen Sie die Erkennung."],
    ["Available memory: {available} GiB; at least {required} GiB is needed.", "当前可用内存 {available} GiB，至少需要 {required} GiB。", "目前可用記憶體 {available} GiB，至少需要 {required} GiB。", "利用可能メモリは {available} GiB、必要量は {required} GiB です。", "Verfügbarer Arbeitsspeicher: {available} GiB; erforderlich: mindestens {required} GiB."],
    ["Installed memory: {available} GiB; automatic recommendations need {required} GiB.", "总内存 {available} GiB，自动推荐需要至少 {required} GiB。", "總記憶體 {available} GiB，自動推薦需要至少 {required} GiB。", "搭載メモリは {available} GiB、自動推奨には {required} GiB が必要です。", "Installierter Arbeitsspeicher: {available} GiB; automatische Empfehlungen erfordern {required} GiB."],
    ["CPU logical threads: {available}; this inference path needs at least {required}.", "CPU 逻辑线程 {available} 个，此推理路径至少需要 {required} 个。", "CPU 邏輯執行緒 {available} 個，此推論路徑至少需要 {required} 個。", "CPU 論理スレッド数は {available}、この推論経路には {required} 以上が必要です。", "Logische CPU-Threads: {available}; dieser Inferenzpfad benötigt mindestens {required}."],
    ["No compatible local runtime is available for this system or architecture.", "当前系统或架构没有兼容的本地推理运行时。", "目前系統或架構沒有相容的本機推論執行階段。", "このシステムまたはアーキテクチャに対応するローカルランタイムはありません。", "Für dieses System oder diese Architektur ist keine kompatible lokale Laufzeit verfügbar."],
    ["Close some apps and detect again, or continue using cloud models.", "可关闭部分应用后重新检测，或继续使用云端模型。", "可關閉部分應用後重新偵測，或繼續使用雲端模型。", "アプリを閉じて再検出するか、クラウドモデルを使ってください。", "Schließen Sie einige Anwendungen und prüfen Sie erneut oder nutzen Sie Cloud-Modelle."],
    ["Installation space for {model}: {required} GiB required, {available} GiB available.", "{model} 安装空间：需要 {required} GiB，可用 {available} GiB。", "{model} 安裝空間：需要 {required} GiB，可用 {available} GiB。", "{model} の導入領域: 必要 {required} GiB、空き {available} GiB。", "Installationsplatz für {model}: {required} GiB benötigt, {available} GiB verfügbar."],
    ["Some required device or disk information is unavailable. Detect again before downloading.", "暂时无法确认必要的设备或磁盘信息，请重新检测后再下载。", "暫時無法確認必要的裝置或磁碟資訊，請重新偵測後再下載。", "必要な端末情報またはディスク情報が不明です。再検出してからダウンロードしてください。", "Erforderliche Geräte- oder Datenträgerinformationen fehlen. Prüfen Sie vor dem Download erneut."],
    ["Available memory could not be read. Detect again before downloading.", "无法读取当前可用内存，请重新检测后再下载。", "無法讀取目前可用記憶體，請重新偵測後再下載。", "現在の利用可能なメモリを取得できません。再検出してからダウンロードしてください。", "Der verfügbare Arbeitsspeicher konnte nicht gelesen werden. Prüfen Sie vor dem Download erneut."],
    ["An existing healthy model is being reused.", "优先复用当前已健康运行的模型。", "優先重用目前正常執行的模型。", "正常に稼働している既存モデルを再利用します。", "Das bereits laufende, gesunde Modell wird wiederverwendet."],
    ["Q8_0 may respond more slowly on CPU.", "Q8_0 在 CPU 上响应可能更慢。", "Q8_0 在 CPU 上的回應可能較慢。", "CPU では Q8_0 の応答が遅くなる場合があります。", "Q8_0 kann auf der CPU langsamer antworten."],
    ["You can manually try this model, but automatic installation is not recommended.", "可手动试用此模型，但不建议自动安装。", "可手動試用此模型，但不建議自動安裝。", "手動で試用できますが、自動導入は推奨しません。", "Sie können dieses Modell manuell testen; eine automatische Installation wird jedoch nicht empfohlen."],
    ["Local response experience", "本地响应体验", "本機回應體驗", "ローカル応答性能", "Lokale Antwortleistung"],
    ["Performance pending confirmation", "性能待确认", "效能待確認", "性能確認待ち", "Leistung noch unbestätigt"],
    ["Response performance checked", "响应性能检查通过", "回應效能檢查通過", "応答性能を確認済み", "Antwortleistung geprüft"],
    ["Slow local responses; automatic hybrid use is not recommended", "本地响应较慢，不建议自动用于端云协同", "本機回應較慢，不建議自動用於端雲協同", "応答が遅いため、自動ハイブリッド利用は推奨しません", "Langsame lokale Antworten; automatische Hybridnutzung wird nicht empfohlen"],
    ["Check local response experience", "检查本地响应体验", "檢查本機回應體驗", "ローカル応答性能を確認", "Lokale Antwortleistung prüfen"],
    ["Performance is not confirmed; automatic hybrid use stays off.", "性能尚未确认，暂不开启自动端云协同。", "效能尚未確認，暫不開啟自動端雲協同。", "性能が未確認のため、自動ハイブリッド利用は無効のままです。", "Leistung noch nicht bestätigt; automatische Hybridnutzung bleibt aus."],
    ["Resource thresholds are conservative estimates for a 64K context; actual performance needs verification.", "资源门槛按 64K 上下文保守估算，实际性能需要运行验证。", "資源門檻依 64K 上下文保守估算，實際效能需執行驗證。", "リソース基準は 64K コンテキストの保守的な推定です。実際の性能は検証が必要です。", "Die Ressourcenwerte sind konservative Schätzungen für 64K Kontext; die tatsächliche Leistung muss geprüft werden."],
    ["Available GPU memory is low. Close GPU-intensive apps and detect again.", "当前空闲显存不足，可关闭占用显卡的应用后重新检测。", "目前空閒顯存不足，可關閉使用顯卡的應用後重新偵測。", "GPU の空きメモリが不足しています。GPU を使うアプリを閉じて再検出してください。", "Es ist zu wenig GPU-Speicher frei. Schließen Sie GPU-intensive Anwendungen und prüfen Sie erneut."],
    ['Detect device', '检测设备', '偵測裝置', '端末を検出', 'Gerät prüfen'],
    ['Configured on-device models', '已配置的端侧模型', '已配置的端側模型', '設定済みのオンデバイスモデル', 'Konfigurierte On-Device-Modelle'],
    ['Select a model', '选择模型', '選擇模型', 'モデルを選択', 'Modell auswählen'],
    ['Enable local model', '启用本地模型', '啟用本機模型', 'ローカルモデルを有効化', 'Lokales Modell aktivieren'],
    ['Download and install', '下载并安装', '下載並安裝', 'ダウンロードしてインストール', 'Herunterladen und installieren'],
    ['Cancel download', '取消下载', '取消下載', 'ダウンロードを中止', 'Download abbrechen'],
    ['Load runtime', '加载运行时', '載入執行階段', 'ランタイムをロード', 'Laufzeit laden'],
    ['Disable and drain', '关闭并排空请求', '關閉並排空請求', '無効化して処理を終了', 'Deaktivieren und Anfragen abschließen'],
    ['Check process health', '检测进程健康', '檢查程序健康', 'プロセス状態を確認', 'Prozesszustand prüfen'],
    ['Check capabilities', '检测能力', '檢查能力', '機能を確認', 'Fähigkeiten prüfen'],
    ['Enable experimental routing and execution', '开启实验性判断与执行', '啟用實驗性判斷與執行', '実験的な判断と実行を有効化', 'Experimentelles Routing und Ausführung aktivieren'],
    ['Disable experimental routing and execution', '关闭实验性判断与执行', '停用實驗性判斷與執行', '実験的な判断と実行を無効化', 'Experimentelles Routing und Ausführung deaktivieren'],
    ['Checks passed · experimental use · quality pending', '功能检查通过 · 可实验使用 · 质量尚未正式验收', '功能檢查通過 · 可實驗使用 · 品質尚未正式驗收', '検査済み・試験利用可・品質受け入れ待ち', 'Prüfung bestanden · experimentell nutzbar · Qualität ausstehend'],
    ['User intent', '用户启用意图', '使用者啟用意圖', 'ユーザーの有効化設定', 'Aktivierungswunsch'],
    ['Installation', '安装状态', '安裝狀態', 'インストール状態', 'Installation'],
    ['Runtime state', '运行状态', '執行狀態', '実行状態', 'Laufzeitstatus'],
    ['Runtime dependencies', '运行时依赖', '執行階段相依套件', 'ランタイムの依存関係', 'Laufzeitabhängigkeiten'],
    ['Not required on this platform', '此平台无需安装', '此平台無需安裝', 'この環境では不要です', 'Auf dieser Plattform nicht erforderlich'],
    ['Preparing runtime dependencies', '正在准备运行时依赖', '正在準備執行階段相依套件', '依存関係を準備しています', 'Laufzeitabhängigkeiten werden vorbereitet'],
    ['Runtime dependencies are missing.', '运行时依赖尚未安装。', '執行階段相依套件尚未安裝。', 'ランタイムの依存関係が未インストールです。', 'Laufzeitabhängigkeiten fehlen.'],
    ['Runtime dependency verification failed.', '运行时依赖校验失败。', '執行階段相依套件驗證失敗。', '依存関係の検証に失敗しました。', 'Die Prüfung der Laufzeitabhängigkeiten ist fehlgeschlagen.'],
    ['Runtime dependency download failed.', '运行时依赖下载失败。', '執行階段相依套件下載失敗。', 'ランタイムの依存関係をダウンロードできませんでした。', 'Der Download der Laufzeitabhängigkeiten ist fehlgeschlagen.'],
    ['Runtime dependencies are damaged.', '运行时依赖已损坏。', '執行階段相依套件已損毀。', 'ランタイムの依存関係が破損しています。', 'Laufzeitabhängigkeiten sind beschädigt.'],
    ['Runtime dependency architecture does not match.', '运行时依赖架构不匹配。', '執行階段相依套件架構不符。', '依存関係のアーキテクチャが一致しません。', 'Die Architektur der Laufzeitabhängigkeiten stimmt nicht überein.'],
    ['Runtime dependency version does not match.', '运行时依赖版本不匹配。', '執行階段相依套件版本不符。', '依存関係のバージョンが一致しません。', 'Die Version der Laufzeitabhängigkeiten stimmt nicht überein.'],
    ['Runtime dependency layout is invalid.', '运行时依赖目录结构无效。', '執行階段相依套件目錄結構無效。', '依存関係の配置が無効です。', 'Die Verzeichnisstruktur der Laufzeitabhängigkeiten ist ungültig.'],
    ['Enable the local model, then use Download and install to repair runtime dependencies.', '请启用本地模型，然后使用“下载并安装”修复运行时依赖。', '請啟用本機模型，然後使用「下載並安裝」修復執行階段相依套件。', 'ローカルモデルを有効にし、「ダウンロードしてインストール」で依存関係を修復してください。', 'Aktivieren Sie das lokale Modell und reparieren Sie die Abhängigkeiten mit „Herunterladen und installieren“.'],
    ['Judgment', '判断能力', '判斷能力', '判断機能', 'Beurteilung'],
    ['Execution', '执行能力', '執行能力', '実行機能', 'Ausführung'],
    ['Redaction', '脱敏能力', '去識別化能力', '匿名化機能', 'Anonymisierung'],
    ['Queue occupancy', '执行 / 等待', '執行 / 等待', '実行 / 待機', 'Aktiv / Wartend'],
    ['Not installed', '未安装', '未安裝', '未インストール', 'Nicht installiert'],
    ['Installed and verified', '已安装并校验', '已安裝並驗證', 'インストール・検証済み', 'Installiert und geprüft'],
    ['Downloading', '下载中', '下載中', 'ダウンロード中', 'Wird heruntergeladen'],
    ['Canceled', '已取消', '已取消', '中止済み', 'Abgebrochen'],
    ['Unloaded', '未加载', '未載入', '未ロード', 'Nicht geladen'],
    ['Loading model', '加载中', '載入中', 'ロード中', 'Wird geladen'],
    ['In use', '使用中', '使用中', '使用中', 'In Verwendung'],
    ['Idle', '空闲', '閒置', 'アイドル', 'Leerlauf'],
    ['Unloading', '卸载中', '卸載中', '解放中', 'Wird entladen'],
    ['Fault', '故障', '故障', '障害', 'Fehler'],
    ['Unknown', '未知', '未知', '不明', 'Unbekannt'],
    ['Unavailable', '不可用', '無法使用', '利用不可', 'Nicht verfügbar'],
    ['Enabled', '已启用', '已啟用', '有効', 'Aktiviert'],
    ['Disabled', '已禁用', '已停用', '無効', 'Deaktiviert'],
    ['Device recommendation', '设备推荐', '裝置建議', '端末の推奨', 'Geräteempfehlung'],
    ['Checking your computer configuration…', '正在检测您的电脑配置…', '正在偵測您的電腦配置…', 'コンピューターの構成を確認しています…', 'Die Computerkonfiguration wird geprüft…'],
    ['Check your computer configuration to choose a local model.', '检测电脑配置，为您推荐适合的本地模型。', '偵測電腦配置，為您建議適合的本機模型。', '構成を確認して、適したローカルモデルを選びましょう。', 'Prüfen Sie die Computerkonfiguration für eine passende Modellempfehlung.'],
    ['Based on the check, your computer is suitable for {model}.', '根据检测，您的电脑配置适合 {model}。', '根據偵測，您的電腦配置適合 {model}。', '確認の結果、このコンピューターには {model} が適しています。', 'Laut Prüfung eignet sich Ihr Computer für {model}.'],
    ['Hardware recommendation: {model}. Complete the device checks before downloading.', '硬件推荐：{model}。请先完成设备检查，再下载模型。', '硬體建議：{model}。請先完成裝置檢查，再下載模型。', 'ハードウェアの推奨：{model}。ダウンロード前に端末の検査を完了してください。', 'Hardwareempfehlung: {model}. Schließen Sie vor dem Download die Geräteprüfung ab.'],
    ['Your computer does not meet the requirements for these local models.', '您的电脑配置暂不满足这些本地模型的运行要求。', '您的電腦配置暫不符合這些本機模型的執行需求。', 'このコンピューターは、これらのローカルモデルの動作要件を満たしていません。', 'Ihr Computer erfüllt die Anforderungen dieser lokalen Modelle nicht.'],
    ['Your computer configuration could not be fully detected. Please try again.', '暂时无法完整检测您的电脑配置，请重新检测。', '暫時無法完整偵測您的電腦配置，請重新偵測。', 'コンピューターの構成を完全に確認できませんでした。再度お試しください。', 'Die Computerkonfiguration konnte nicht vollständig ermittelt werden. Bitte erneut prüfen.'],
    ['Device detection failed. Please try again.', '设备检测失败，请重新检测。', '裝置偵測失敗，請重新偵測。', '端末の検出に失敗しました。再度お試しください。', 'Die Geräteerkennung ist fehlgeschlagen. Bitte erneut prüfen.'],
    ['You can also choose {model} to use fewer resources.', '也可选择 {model}，减少资源占用。', '也可選擇 {model}，減少資源用量。', 'リソース使用量を抑えるには {model} も選べます。', 'Für einen geringeren Ressourcenverbrauch können Sie auch {model} wählen.'],
    ['You can also try {model}, which uses more memory and runs more slowly on 16 GB devices.', '也可手动尝试 {model}，但在 16 GB 设备上内存占用更高、推理更慢。', '也可手動嘗試 {model}，但在 16 GB 裝置上記憶體用量更高、推理較慢。', '手動で {model} も試せますが、16 GB 端末ではメモリ使用量が増え、動作が遅くなります。', 'Sie können auch {model} testen; auf 16-GB-Geräten benötigt es mehr Speicher und läuft langsamer.'],
    ['Available memory or CPU threads are limited, so only {model} is enabled.', '当前可用内存或 CPU 线程受限，仅支持启用 {model}。', '目前可用記憶體或 CPU 執行緒受限，僅支援啟用 {model}。', '利用可能なメモリまたは CPU スレッドが限られているため、{model} のみ有効化できます。', 'Verfügbarer Speicher oder CPU-Threads sind begrenzt; daher ist nur {model} aktivierbar.'],
    ['Free up available memory and detect again.', '请释放可用内存后重新检测。', '請釋放可用記憶體後重新偵測。', '空きメモリを確保してから再度検出してください。', 'Geben Sie Arbeitsspeicher frei und prüfen Sie erneut.'],
    ['Device check did not pass. Detect again and check memory, backend and free disk space.', '设备检查未通过。请重新检测并检查内存、后端和可用磁盘空间。', '裝置檢查未通過。請重新偵測並檢查記憶體、後端及可用磁碟空間。', '端末検査に失敗しました。メモリ、バックエンド、空き容量を確認してください。', 'Geräteprüfung fehlgeschlagen. Speicher, Backend und freien Platz prüfen.'],
    ['Capability acceptance suites have not been provided.', '尚未提供专项能力验收规范与样例。', '尚未提供專項能力驗收規範與樣例。', '機能の検証仕様とサンプルは未提供です。', 'Abnahmespezifikationen und Beispiele fehlen.'],
    ['The selected model does not support this operation.', '所选模型不支持此操作。', '所選模型不支援此操作。', '選択したモデルはこの操作に対応していません。', 'Das gewählte Modell unterstützt diesen Vorgang nicht.'],
    ['Local model operation failed. Refresh to inspect status.', '本地模型操作失败，请刷新查看状态。', '本機模型操作失敗，請重新整理檢視狀態。', '操作に失敗しました。更新して状態を確認してください。', 'Vorgang fehlgeschlagen. Status aktualisieren.'],
    ['Download interrupted', '下载中断', '下載中斷', 'ダウンロード中断', 'Download unterbrochen'],
    ['[{step}/{total}] Windows VC++ runtime components', '[{step}/{total}] Windows VC++ 运行时组件', '[{step}/{total}] Windows VC++ 執行階段元件', '[{step}/{total}] Windows VC++ ランタイムコンポーネント', '[{step}/{total}] Windows VC++-Laufzeitkomponenten'],
    ['[{step}/{total}] Local inference engine llama.cpp', '[{step}/{total}] 本地推理引擎 llama.cpp', '[{step}/{total}] 本機推理引擎 llama.cpp', '[{step}/{total}] ローカル推論エンジン llama.cpp', '[{step}/{total}] Lokale Inferenz-Engine llama.cpp'],
    ['[{step}/{total}] Model weights ({model})', '[{step}/{total}] 模型权重文件 ({model})', '[{step}/{total}] 模型權重檔案 ({model})', '[{step}/{total}] モデル重みファイル ({model})', '[{step}/{total}] Modellgewichte ({model})'],
    ['[{step}/{total}] Chat template (.jinja)', '[{step}/{total}] 模型对话模板配置 (.jinja)', '[{step}/{total}] 模型對話範本設定 (.jinja)', '[{step}/{total}] チャットテンプレート設定 (.jinja)', '[{step}/{total}] Chat-Vorlagenkonfiguration (.jinja)'],
    ['[{step}/{total}] Verifying and extracting files', '[{step}/{total}] 校验与解压本地运行文件', '[{step}/{total}] 驗證與解壓縮本機執行檔案', '[{step}/{total}] ローカル実行ファイルの検証と展開', '[{step}/{total}] Lokale Laufzeitdateien prüfen und entpacken'],
    ['Source: {host} (Microsoft official source; check system proxy if slow)', '来源：{host}（微软官方源，若缓慢请检查系统代理）', '來源：{host}（微軟官方來源，若緩慢請檢查系統代理）', '取得元：{host}（Microsoft 公式。遅い場合はプロキシを確認）', 'Quelle: {host} (Offizieller Microsoft-Server; bei Langsamkeit Proxy prüfen)'],
    ['Source: {host} (GitHub release; enable VPN or proxy if progress stalls)', '来源：{host}（GitHub 跨境源，若进度卡住请开启 VPN / 代理）', '來源：{host}（GitHub 跨境來源，若進度卡住請開啟 VPN / 代理）', '取得元：{host}（GitHub。進まない場合は VPN / プロキシを有効化）', 'Quelle: {host} (GitHub; bei Stillstand VPN / Proxy aktivieren)'],
    ['Source: {host} (Domestic mirror; resume supported, use direct connection if VPN slows down)', '来源：{host}（国内镜像源，支持断点续传；若开启 VPN 后变慢建议直连）', '來源：{host}（國內鏡像來源，支援斷點續傳；若開啟 VPN 後變慢建議直連）', '取得元：{host}（ミラーサイト・レジューム対応。VPN で遅い場合は直接接続を推奨）', 'Quelle: {host} (Mirror mit Fortsetzung; bei langsamem VPN Direktverbindung nutzen)'],
    ['Source: {host}', '来源：{host}', '來源：{host}', '取得元：{host}', 'Quelle: {host}'],
    ['Stage [1/4] Windows VC++ runtime (aka.ms) download failed: connection to Microsoft timed out or was interrupted. Check your network or system proxy/VPN settings, then click "Download and install" to retry.', '阶段 [1/4] Windows VC++ 运行时组件 (aka.ms) 下载失败：连接微软官方源超时或中断。若开启了全局代理/VPN 导致微软源异常，请尝试切换代理模式或直连后点击“下载并安装”重试。', '階段 [1/4] Windows VC++ 執行階段元件 (aka.ms) 下載失敗：連線微軟官方來源逾時或中斷。若開啟了全域代理/VPN 導致微軟來源異常，請嘗試切換代理模式或直連後點擊「下載並安裝」重試。', '段階 [1/4] Windows VC++ ランタイム (aka.ms) のダウンロード失敗：Microsoft 公式サーバーへの接続がタイムアウトまたは中断しました。プロキシ / VPN 設定を確認し、「ダウンロードしてインストール」で再試行してください。', 'Phase [1/4] Download der Windows VC++-Laufzeit (aka.ms) fehlgeschlagen: Verbindung zu Microsoft abgelaufen oder unterbrochen. Prüfen Sie Proxy-/VPN-Einstellungen und klicken Sie auf „Herunterladen und installieren“.'],
    ['Stage [1/4] Windows VC++ runtime verification failed: package is incomplete or modified in transit. Click "Download and install" to fetch it again.', '阶段 [1/4] Windows VC++ 运行时组件校验失败：安装包不完整或被网络劫持修改，请点击“下载并安装”重新获取。', '階段 [1/4] Windows VC++ 執行階段元件驗證失敗：安裝包不完整或遭網路劫持修改，請點擊「下載並安裝」重新取得。', '段階 [1/4] Windows VC++ ランタイムの検証失敗：パッケージが不完全または破損しています。「ダウンロードしてインストール」で再取得してください。', 'Phase [1/4] Prüfung der Windows VC++-Laufzeit fehlgeschlagen: Paket unvollständig oder beschädigt. Klicken Sie zur Wiederholung auf „Herunterladen und installieren“.'],
    ['Stage [2/4] Inference engine (github.com) download timed out: direct connection to GitHub is too slow or unreachable. Enable your VPN or proxy for github.com, then click "Download and install" to continue.', '阶段 [2/4] 推理引擎 (github.com) 下载超时：直连 GitHub 速度过慢或无法连接。请开启 VPN / 代理（确保 github.com 走代理通道），然后点击“下载并安装”继续。', '階段 [2/4] 推理引擎 (github.com) 下載逾時：直連 GitHub 速度過慢或無法連線。請開啟 VPN / 代理（確保 github.com 走代理通道），然後點擊「下載並安裝」繼續。', '段階 [2/4] 推論エンジン (github.com) のダウンロードがタイムアウトしました：GitHub への直接接続が遅いか到達できません。VPN またはプロキシを有効にしてから「ダウンロードしてインストール」をクリックしてください。', 'Phase [2/4] Zeitüberschreitung beim Download der Inferenz-Engine (github.com): Direktverbindung zu GitHub zu langsam oder nicht erreichbar. Aktivieren Sie VPN/Proxy für github.com und klicken Sie auf „Herunterladen und installieren“.'],
    ['Stage [2/4] Inference engine (github.com) connection interrupted: network link is unstable. Enable your VPN or switch to a stable proxy node, then click "Download and install" (partial progress will resume automatically).', '阶段 [2/4] 推理引擎 (github.com) 连接意外断开：跨境网络链路不稳定。请开启 VPN 或切换更稳定的代理节点后点击“下载并安装”（已下载部分支持自动断点续传）。', '階段 [2/4] 推理引擎 (github.com) 連線意外中斷：跨境網路鏈路不穩定。請開啟 VPN 或切換更穩定的代理節點後點擊「下載並安裝」（已下載部分支援自動斷點續傳）。', '段階 [2/4] 推論エンジン (github.com) の接続が中断されました：通信が不安定です。VPN を有効化または安定したプロキシに切り替えて「ダウンロードしてインストール」をクリックしてください（自動レジューム対応）。', 'Phase [2/4] Verbindung zur Inferenz-Engine (github.com) unterbrochen: Netzwerk instabil. Aktivieren Sie VPN oder wechseln Sie den Proxy-Knoten und klicken Sie auf „Herunterladen und installieren“ (automatische Fortsetzung).'],
    ['Stage [2/4] Inference engine (github.com) download rejected by server: proxy error or GitHub rate limit. Switch your VPN node and retry.', '阶段 [2/4] 推理引擎 (github.com) 下载被拒绝：代理服务器异常或 GitHub 节点受限，请更换 VPN 节点后重试。', '階段 [2/4] 推理引擎 (github.com) 下載遭拒：代理伺服器異常或 GitHub 節點受限，請更換 VPN 節點後重試。', '段階 [2/4] 推論エンジン (github.com) のダウンロードが拒否されました：プロキシエラーまたは制限です。VPN ノードを切り替えて再試行してください。', 'Phase [2/4] Download der Inferenz-Engine (github.com) vom Server abgelehnt: Proxy-Fehler oder Limit. Wechseln Sie den VPN-Knoten und versuchen Sie es erneut.'],
    ['Stage [2/4] Inference engine (github.com) verification failed: downloaded archive is corrupted. Check proxy stability and click "Download and install" to retry.', '阶段 [2/4] 推理引擎 (github.com) 文件校验失败：下载数据损坏，请检查代理稳定性后点击“下载并安装”重新获取。', '階段 [2/4] 推理引擎 (github.com) 檔案驗證失敗：下載資料損毀，請檢查代理穩定性後點擊「下載並安裝」重新取得。', '段階 [2/4] 推論エンジン (github.com) のファイル検証失敗：データが破損しています。プロキシの安定性を確認し、再試行してください。', 'Phase [2/4] Dateiprüfung der Inferenz-Engine (github.com) fehlgeschlagen: Archiv beschädigt. Prüfen Sie die Proxy-Stabilität und versuchen Sie es erneut.'],
    ['Stage [3/4] Model weights (hf-mirror.com) download interrupted at {progress}. If your VPN slows down or drops domestic mirror traffic, set hf-mirror.com to Direct or disable VPN, then click "Download and install" to resume.', '阶段 [3/4] 模型权重文件 (hf-mirror.com) 下载中断：当前已保存进度 {progress}。若开启 VPN 后国内镜像变慢或断开，建议将 hf-mirror.com 设为直连（Direct）或关闭 VPN，再点击“下载并安装”从断点继续下载。', '階段 [3/4] 模型權重檔案 (hf-mirror.com) 下載中斷：目前已儲存進度 {progress}。若開啟 VPN 後國內鏡像變慢或中斷，建議將 hf-mirror.com 設為直連（Direct）或關閉 VPN，再點擊「下載並安裝」從斷點繼續下載。', '段階 [3/4] モデル重みファイル (hf-mirror.com) のダウンロードが {progress} で中断しました。VPN によりミラー接続が不安定な場合は hf-mirror.com を直接接続（Direct）に設定するか VPN をオフにし、「ダウンロードしてインストール」で再開してください。', 'Phase [3/4] Download der Modellgewichte (hf-mirror.com) bei {progress} unterbrochen. Falls das VPN den Mirror verlangsamt, stellen Sie hf-mirror.com auf Direktverbindung oder deaktivieren Sie das VPN und klicken Sie zum Fortsetzen auf „Herunterladen und installieren“.'],
    ['Stage [3/4] Model weights (hf-mirror.com) request failed: mirror connection blocked. Check proxy routing rules and click "Download and install" to resume.', '阶段 [3/4] 模型权重文件 (hf-mirror.com) 请求失败：镜像源连接受阻，请检查代理分流设置后点击“下载并安装”继续（支持断点续传）。', '階段 [3/4] 模型權重檔案 (hf-mirror.com) 請求失敗：鏡像來源連線受阻，請檢查代理分流設定後點擊「下載並安裝」繼續（支援斷點續傳）。', '段階 [3/4] モデル重みファイル (hf-mirror.com) のリクエスト失敗：ミラー接続がブロックされました。プロキシ設定を確認して再開してください。', 'Phase [3/4] Anfrage für Modellgewichte (hf-mirror.com) fehlgeschlagen: Mirror-Verbindung blockiert. Prüfen Sie die Proxy-Regeln und setzen Sie den Download fort.'],
    ['Stage [3/4] Model weights (.gguf) SHA256 verification failed: file was corrupted in transit and cleaned up. Click "Download and install" to download again.', '阶段 [3/4] 模型权重文件 (.gguf) SHA256 校验不一致：文件在传输中损坏，系统已清理损坏缓存，请点击“下载并安装”重新下载。', '階段 [3/4] 模型權重檔案 (.gguf) SHA256 驗證不一致：檔案在傳輸中損毀，系統已清理損毀快取，請點擊「下載並安裝」重新下載。', '段階 [3/4] モデル重みファイル (.gguf) の SHA256 検証失敗：転送中に破損したためキャッシュを消去しました。「ダウンロードしてインストール」で再取得してください。', 'Phase [3/4] SHA256-Prüfung der Modellgewichte (.gguf) fehlgeschlagen: Datei beim Transfer beschädigt und bereinigt. Klicken Sie zum erneuten Download auf „Herunterladen und installieren“.'],
    ['Stage [4/4] Chat template (hf-mirror.com) download failed: model weights are ready and only the tiny template file remains. Check your network and click "Download and install" to finish immediately.', '阶段 [4/4] 模型对话模板 (hf-mirror.com) 下载失败：模型权重已就绪，仅最后一步配置文件未完成。请检查网络后点击“下载并安装”即可秒级完成。', '階段 [4/4] 模型對話範本 (hf-mirror.com) 下載失敗：模型權重已就緒，僅最後一步設定檔未完成。請檢查網路後點擊「下載並安裝」即可秒級完成。', '段階 [4/4] チャットテンプレート (hf-mirror.com) のダウンロード失敗：モデル重みは完了しており、小さな設定ファイルのみ残っています。ネットワークを確認して「ダウンロードしてインストール」をクリックしてください。', 'Phase [4/4] Download der Chat-Vorlage (hf-mirror.com) fehlgeschlagen: Modellgewichte sind bereit, es fehlt nur die Vorlagendatei. Prüfen Sie das Netzwerk und klicken Sie auf „Herunterladen und installieren“.'],
    ['Insufficient local disk space: unable to write model or runtime files. Free up disk space for the selected model and runtime, then retry.', '本地磁盘空间不足：无法写入模型或运行时文件。请为所选模型和运行时清理足够的磁盘空间后重试。', '本機磁碟空間不足：無法寫入模型或執行階段檔案。請為所選模型和執行階段釋放足夠的磁碟空間後重試。', 'ディスク空き容量不足です。選択したモデルとランタイムに十分な空き容量を確保して再試行してください。', 'Unzureichender Speicherplatz. Geben Sie ausreichend Platz für das gewählte Modell und die Laufzeit frei und versuchen Sie es erneut.'],
    ['Local installation verification or extraction failed: files may be in use, blocked by security software, or out of disk space. Check disk space and antivirus logs, then click "Download and install" to retry.', '本地安装校验或解压失败：运行文件可能被占用、杀毒软件拦截或磁盘空间不足。请检查磁盘空间与安全软件拦截记录后点击“下载并安装”重试。', '本機安裝驗證或解壓縮失敗：執行檔案可能被佔用、防毒軟體攔截或磁碟空間不足。請檢查磁碟空間與安全軟體攔截紀錄後點擊「下載並安裝」重試。', 'ローカル検証または展開に失敗しました：ファイルが使用中、セキュリティソフトによるブロック、または容量不足の可能性があります。確認後「ダウンロードしてインストール」で再試行してください。', 'Lokale Prüfung oder Entpacken fehlgeschlagen: Dateien werden evtl. verwendet, durch Sicherheitssoftware blockiert oder der Speicherplatz reicht nicht aus. Prüfen Sie dies und klicken Sie auf „Herunterladen und installieren“.'],
    ['Public execution weights; judgment and redaction need separate acceptance evidence.', '公开执行权重；判断和脱敏需要独立验收证据。', '公開執行權重；判斷及去識別化需要獨立驗收證據。', '公開の実行用重み。判断と匿名化には個別の検証が必要です。', 'Öffentliche Ausführungsgewichte; Beurteilung und Anonymisierung brauchen eigene Nachweise.'],
    ['Opening this page does not enable or download a model. Closing the page keeps 端云协同 running.', '打开页面不会启用或下载模型。关闭页面后 端云协同 继续运行。', '開啟頁面不會啟用或下載模型。關閉頁面後 端云协同 繼續執行。', 'ページを開いてもモデルは有効化・ダウンロードされません。閉じても端云协同は動作します。', 'Diese Seite aktiviert oder lädt kein Modell. 端云协同 läuft nach dem Schließen weiter.'],
    ['Enabled models stay loaded while 端云协同 runs. Your on/off choice is remembered at the next startup.', '模型开启后保持加载，空闲时不会自动卸载。下次启动会记住您的开关选择。', '模型開啟後保持載入，閒置時不會自動卸載。下次啟動會記住您的開關選擇。', '有効なモデルは端云协同の実行中に保持され、アイドル時も解放されません。次回起動時もオン・オフの選択が保存されます。', 'Aktivierte Modelle bleiben geladen, solange 端云协同 läuft. Ihre Ein-/Aus-Auswahl wird beim nächsten Start beibehalten.'],
  ]) for (const [lang, text] of [['zh', zh], ['zh-TW', tw], ['ja', ja], ['de', de]]) (I18N[lang] ||= {})[key] = text;

  let data = null, device = null, working = false;
  let stream, refreshController, retryTimer, readGeneration = 0, retryDelay = 1000, redactionKey = '';
  let detectionAttempted = false, detecting = false, detectionFailed = false, connection = 0, selectedModelId = '';
  const stateNames = { not_installed: 'Not installed', installed: 'Installed and verified', downloading: 'Downloading', canceled: 'Canceled', failed: 'Failed', unloaded: 'Unloaded', loading: 'Loading model', ready: 'Ready', busy: 'In use', idle: 'Idle', unloading: 'Unloading', fault: 'Fault', unknown: 'Unknown', available: 'Ready', unavailable: 'Unavailable' };
  const label = value => t(stateNames[value] || 'Unknown');
  const dependencyNames = { not_required: 'Not required on this platform', missing: 'Runtime dependencies are missing.', preparing: 'Preparing runtime dependencies', ready: 'Installed and verified', canceled: 'Canceled', failed: 'Runtime dependency download failed.', download_failed: 'Runtime dependency download failed.', corrupt: 'Runtime dependencies are damaged.', architecture: 'Runtime dependency architecture does not match.', version: 'Runtime dependency version does not match.', layout: 'Runtime dependency layout is invalid.' };
  const dependencyMessage = code => dependencyNames[String(code || '').replace(/^local_model_dependency_/, '')] || 'Runtime dependency verification failed.';
  const modelName = tier => tier === '4B' ? 'Spark-X2.6-4B-Q4_K_M' : undefined;
  const displayModelName = model => model?.name || modelName(model?.tier) || model?.id;
  function currentChoiceModelId(status) {
    const catalogModels = data?.catalog?.models || [];
    if (selectedModelId && catalogModels.some(m => m.id === selectedModelId)) return selectedModelId;
    if (status?.installingModel && catalogModels.some(m => m.id === status.installingModel)) return status.installingModel;
    return status?.intent?.selected || '';
  }
  function choiceInstallationState(status, choiceModelId) {
    if (!choiceModelId) return status?.installation || 'not_installed';
    const choiceId = String(choiceModelId).toLowerCase();
    const selectedId = String(status?.intent?.selected || '').toLowerCase();
    const installingId = String(status?.installingModel || '').toLowerCase();
    if (installingId) {
      if (choiceId === installingId) return status?.installation || 'not_installed';
    } else if (choiceId === selectedId && status?.installation && status.installation !== 'unknown') {
      return status.installation;
    }
    const cand = (data?.models || []).find(item => String(item.id || '').toLowerCase() === choiceId);
    return cand?.installed ? 'installed' : 'not_installed';
  }
  function resolveProgressMeta(status, dependencies) {
    const p = status?.progress || {};
    const hasDeps = dependencies?.state && dependencies.state !== 'not_required';
    const totalSteps = Number(p.totalSteps) || (hasDeps ? 4 : 3);
    const offset = totalSteps === 4 ? 1 : 0;
    let stage = p.stage || '';
    if (!stage && p.artifact) {
      const art = String(p.artifact).toLowerCase();
      if (art.includes('vc') || art === 'dep' || art.includes('redist')) stage = 'dependency';
      else if (art.includes('llama') || art === 'runtime') stage = 'runtime';
      else if (art.includes('template') || art.includes('jinja')) stage = 'template';
      else if (art.includes('model') || art.includes('spark') || art.includes('gguf')) stage = 'model';
    }
    const defaultSteps = { dependency: 1, runtime: offset + 1, model: offset + 2, template: offset + 3, verify: totalSteps };
    const step = Number(p.step) || defaultSteps[stage] || 1;
    const defaultHosts = { dependency: 'aka.ms', runtime: 'github.com', model: 'hf-mirror.com', template: 'hf-mirror.com' };
    const host = p.host || defaultHosts[stage] || '';
    const targetModelId = status?.installingModel || status?.intent?.selected;
    const selectedModel = displayModelName(data?.catalog?.models?.find(m => m.id === targetModelId)) || targetModelId || 'GGUF';
    const stageKeys = {
      dependency: '[{step}/{total}] Windows VC++ runtime components',
      runtime: '[{step}/{total}] Local inference engine llama.cpp',
      model: '[{step}/{total}] Model weights ({model})',
      template: '[{step}/{total}] Chat template (.jinja)',
      verify: '[{step}/{total}] Verifying and extracting files',
    };
    const stageText = stageKeys[stage] ? t(stageKeys[stage], { step, total: totalSteps, model: selectedModel }) : '';
    let hostHint = '';
    if (host) {
      if (host.includes('aka.ms') || host.includes('microsoft.com') || host.includes('visualstudio.microsoft.com')) {
        hostHint = t('Source: {host} (Microsoft official source; check system proxy if slow)', { host });
      } else if (host.includes('github.com') || host.includes('githubusercontent.com')) {
        hostHint = t('Source: {host} (GitHub release; enable VPN or proxy if progress stalls)', { host });
      } else if (host.includes('hf-mirror.com')) {
        hostHint = t('Source: {host} (Domestic mirror; resume supported, use direct connection if VPN slows down)', { host });
      } else {
        hostHint = t('Source: {host}', { host });
      }
    }
    return { stage, step, totalSteps, host, stageText, hostHint };
  }
  function statusErrorMessage(status) {
    const code = String(status?.error || '');
    if (!code) return '';
    const downloaded = Number(status?.progress?.downloaded) || 0;
    const total = Number(status?.progress?.total) || 0;
    const pct = total > 0 ? Math.min(100, Math.max(0, Math.round((downloaded / total) * 100))) : 0;
    const progressText = total > 0 ? `${(downloaded / 1048576).toFixed(1)} / ${(total / 1048576).toFixed(1)} MiB (${pct}%)` : '0.0 MiB';
    if (code.endsWith('_disk_space') || status?.progress?.reason === 'disk_space') {
      return t('Insufficient local disk space: unable to write model or runtime files. Free up disk space for the selected model and runtime, then retry.');
    }
    if (code.startsWith('local_model_dependency_')) {
      if (status?.installation === 'failed' && status?.progress?.stage === 'dependency') {
        if (code === 'local_model_dependency_corrupt') {
          return t('Stage [1/4] Windows VC++ runtime verification failed: package is incomplete or modified in transit. Click "Download and install" to fetch it again.');
        }
        if (code === 'local_model_dependency_download_failed') {
          return t('Stage [1/4] Windows VC++ runtime (aka.ms) download failed: connection to Microsoft timed out or was interrupted. Check your network or system proxy/VPN settings, then click "Download and install" to retry.');
        }
      }
      return t(dependencyMessage(code));
    }
    const stageMessages = {
      local_model_runtime_timeout: 'Stage [2/4] Inference engine (github.com) download timed out: direct connection to GitHub is too slow or unreachable. Enable your VPN or proxy for github.com, then click "Download and install" to continue.',
      local_model_runtime_download_failed: 'Stage [2/4] Inference engine (github.com) download timed out: direct connection to GitHub is too slow or unreachable. Enable your VPN or proxy for github.com, then click "Download and install" to continue.',
      local_model_runtime_interrupted: 'Stage [2/4] Inference engine (github.com) connection interrupted: network link is unstable. Enable your VPN or switch to a stable proxy node, then click "Download and install" (partial progress will resume automatically).',
      local_model_runtime_http_status: 'Stage [2/4] Inference engine (github.com) download rejected by server: proxy error or GitHub rate limit. Switch your VPN node and retry.',
      local_model_runtime_corrupt: 'Stage [2/4] Inference engine (github.com) verification failed: downloaded archive is corrupted. Check proxy stability and click "Download and install" to retry.',
      local_model_weights_timeout: 'Stage [3/4] Model weights (hf-mirror.com) download interrupted at {progress}. If your VPN slows down or drops domestic mirror traffic, set hf-mirror.com to Direct or disable VPN, then click "Download and install" to resume.',
      local_model_weights_interrupted: 'Stage [3/4] Model weights (hf-mirror.com) download interrupted at {progress}. If your VPN slows down or drops domestic mirror traffic, set hf-mirror.com to Direct or disable VPN, then click "Download and install" to resume.',
      local_model_weights_download_failed: 'Stage [3/4] Model weights (hf-mirror.com) download interrupted at {progress}. If your VPN slows down or drops domestic mirror traffic, set hf-mirror.com to Direct or disable VPN, then click "Download and install" to resume.',
      local_model_weights_http_status: 'Stage [3/4] Model weights (hf-mirror.com) request failed: mirror connection blocked. Check proxy routing rules and click "Download and install" to resume.',
      local_model_weights_corrupt: 'Stage [3/4] Model weights (.gguf) SHA256 verification failed: file was corrupted in transit and cleaned up. Click "Download and install" to download again.',
      local_model_template_timeout: 'Stage [4/4] Chat template (hf-mirror.com) download failed: model weights are ready and only the tiny template file remains. Check your network and click "Download and install" to finish immediately.',
      local_model_template_interrupted: 'Stage [4/4] Chat template (hf-mirror.com) download failed: model weights are ready and only the tiny template file remains. Check your network and click "Download and install" to finish immediately.',
      local_model_template_http_status: 'Stage [4/4] Chat template (hf-mirror.com) download failed: model weights are ready and only the tiny template file remains. Check your network and click "Download and install" to finish immediately.',
      local_model_template_corrupt: 'Stage [4/4] Chat template (hf-mirror.com) download failed: model weights are ready and only the tiny template file remains. Check your network and click "Download and install" to finish immediately.',
      local_model_template_download_failed: 'Stage [4/4] Chat template (hf-mirror.com) download failed: model weights are ready and only the tiny template file remains. Check your network and click "Download and install" to finish immediately.',
      local_model_verify_failed: 'Local installation verification or extraction failed: files may be in use, blocked by security software, or out of disk space. Check disk space and antivirus logs, then click "Download and install" to retry.',
    };
    if (stageMessages[code]) {
      return t(stageMessages[code], { progress: progressText });
    }
    return t('Local model operation failed. Refresh to inspect status.');
  }
  function row(name, value) { const node = el('div', 'row pref local-model-status'); const who = el('div', 'who'); who.append(el('div', 'name', t(name))); node.append(who, el('span', 'router-state', value)); return node; }
  function deviceGuide() {
    const guide = el('section', 'local-device-guide'); guide.id = 'localDeviceGuide';
    guide.setAttribute('aria-labelledby', 'localDeviceTitle');
    const heading = el('div', 'local-device-heading');
    const title = el('h3', '', t('Device recommendation')); title.id = 'localDeviceTitle';
    const button = el('button', 'text local-model-action', t('Detect device')); button.type = 'button'; button.dataset.action = 'detect';
    button.disabled = working; button.onclick = detectDevice;
    heading.append(title, button); guide.append(heading);
    const advice = device?.advice;
    const recommended = advice?.modelName || (!advice ? modelName(device?.recommendation?.tier) : undefined);
    let message = 'Check your computer configuration to choose a local model.';
    if (detecting) message = 'Checking your computer configuration…';
    else if (detectionFailed) message = 'Device detection failed. Please try again.';
    else if (advice?.state === 'not_recommended') message = 'Local models are not recommended for this computer. You can use cloud models without downloading one.';
    else if (advice?.state === 'deferred') message = 'Local download is not recommended right now. Check the resource details and detect again.';
    else if (device?.recommendation?.tier === 'none') message = 'Your computer does not meet the requirements for these local models.';
    else if (recommended) message = device.detection?.state === 'passed' ? 'Based on the check, your computer is suitable for {model}.' : 'Hardware recommendation: {model}. Complete the device checks before downloading.';
    else if (device) message = 'Your computer configuration could not be fully detected. Please try again.';
    const text = el('p', 'local-device-message', t(message, { model: recommended })); text.id = 'localDeviceMessage';
    text.setAttribute('role', 'status'); text.setAttribute('aria-live', 'polite'); guide.append(text);
    if (!detecting && !detectionFailed && device?.recommendation?.tier === 'none' && device?.recommendation?.reason === 'available_memory_below_2_5_gib') {
      guide.append(el('p', 'hint router-note', t('Free up available memory and detect again.')));
    }
    if (recommended && !detecting && !detectionFailed) {
      if (device.detection?.state !== 'passed') guide.append(el('p', 'hint router-note', t('Device check did not pass. Detect again and check memory, backend and free disk space.')));
    }
    if (advice && !detecting && !detectionFailed) {
      if (advice.reuse) guide.append(el('p', 'hint router-note', t('An existing healthy model is being reused.')));
      const chosen = currentChoiceModelId(data?.status) || advice.modelID;
      const assessment = advice.assessments?.find(item => item.id === chosen) || advice.assessments?.find(item => item.quantization === 'Q4_K_M');
      const memoryIssue = assessment?.issues?.find(item => item.code === 'memory_below_16_gib');
      if (memoryIssue?.available != null) guide.append(el('p', 'hint router-note', t('Installed memory: {available} GiB; automatic recommendations need {required} GiB.', { available: (memoryIssue.available / 2 ** 30).toFixed(1), required: (memoryIssue.required / 2 ** 30).toFixed(1) })));
      const cpuIssue = assessment?.issues?.find(item => ['cpu_threads_below_4', 'cpu_threads_below_8', 'q8_cpu_threads_or_backend'].includes(item.code));
      const threads = device.device?.system?.value?.logicalCPUs?.value;
      if (cpuIssue && threads != null) guide.append(el('p', 'hint router-note', t('CPU logical threads: {available}; this inference path needs at least {required}.', { available: threads, required: cpuIssue.required })));
      if (assessment?.issues?.some(item => ['runtime_unavailable', 'platform_unsupported', 'os_version_unsupported'].includes(item.code))) guide.append(el('p', 'hint router-note', t('No compatible local runtime is available for this system or architecture.')));
      const issue = assessment?.issues?.find(item => item.code === 'available_memory_insufficient');
      if (issue) {
        guide.append(el('p', 'hint router-note', t('Available memory: {available} GiB; at least {required} GiB is needed.', { available: (issue.available / 2 ** 30).toFixed(1), required: (issue.required / 2 ** 30).toFixed(1) })));
        guide.append(el('p', 'hint router-note', t('Close some apps and detect again, or continue using cloud models.')));
      }
      if (assessment?.issues?.some(item => item.code === 'gpu_memory_insufficient')) guide.append(el('p', 'hint router-note', t('Available GPU memory is low. Close GPU-intensive apps and detect again.')));
      if (assessment?.issues?.some(item => item.code === 'available_memory_unknown')) guide.append(el('p', 'hint router-note', t('Available memory could not be read. Detect again before downloading.')));
      if (assessment?.issues?.some(item => (item.code.endsWith('_unknown') && item.code !== 'available_memory_unknown') || item.code === 'backend_unconfirmed')) guide.append(el('p', 'hint router-note', t('Some required device or disk information is unavailable. Detect again before downloading.')));
      if (advice.state !== 'recommended' && assessment?.manual === 'passed') guide.append(el('p', 'hint router-note', t('You can manually try this model, but automatic installation is not recommended.')));
      if (assessment?.quantization === 'Q8_0' && assessment?.issues?.some(item => item.code === 'q8_acceleration_required')) guide.append(el('p', 'hint router-note', t('Q8_0 may respond more slowly on CPU.')));
      for (const candidate of advice.assessments || []) {
        for (const disk of candidate.disks || []) {
          if (disk.required > 0 && disk.available?.value != null) {
            guide.append(el('p', 'hint router-note', t('Installation space for {model}: {required} GiB required, {available} GiB available.', { model: candidate.name, required: (disk.required / 2 ** 30).toFixed(1), available: (disk.available.value / 2 ** 30).toFixed(1) }) + ' · ' + disk.path));
          }
        }
      }
      guide.append(el('p', 'hint router-note', t('Resource thresholds are conservative estimates for a 64K context; actual performance needs verification.')));
    }
    return guide;
  }
  function installedCatalogModels(status) {
    const catalogModels = data?.catalog?.models || [];
    const candidates = data?.models || [];
    const selectedId = String(status?.intent?.selected || '').toLowerCase();
    const installingId = String(status?.installingModel || '').toLowerCase();
    return catalogModels.filter(model => {
      const id = String(model.id || '').toLowerCase();
      const cand = candidates.find(item => String(item.id || '').toLowerCase() === id);
      if (cand?.installed) return true;
      if (id === selectedId && (!installingId || installingId === selectedId) && status?.installation && status.installation !== 'unknown') {
        return status.installation === 'installed';
      }
      return false;
    });
  }
  function installedModelsSection(status, dependencies) {
    const installed = installedCatalogModels(status);
    if (!installed.length) return null;
    const wrap = el('section', 'local-installed-wrap'); wrap.id = 'localInstalledWrap';
    const head = el('div', 'row-head router-label');
    head.append(el('span', 'label', t('Configured on-device models')));
    const installedList = el('div', 'list'); installedList.id = 'localInstalledModels';
    for (const model of installed) {
      const isSelected = String(status.intent?.selected || '').toLowerCase() === String(model.id || '').toLowerCase();
      const runtimeState = isSelected ? status.runtime : 'unloaded';
      const isEnabled = isSelected && Boolean(status.intent?.enabled);
      const isLoaded = isEnabled && ['ready', 'loading', 'busy', 'idle'].includes(status.runtime);
      const item = el('div', 'row provider local-provider' + (isEnabled ? '' : ' off'));
      item.dataset.localModel = model.id;
      item.dataset.selector = 'local/' + String(model.id || '').toLowerCase();
      const icon = el('span', 'ic'), img = el('img');
      img.src = '/icons/spark-color.svg'; img.alt = ''; icon.append(img);
      const name = displayModelName(model);
      const who = el('div', 'who');
      who.append(el('div', 'name', name), el('div', 'sub', `local/${String(model.id || '').toLowerCase()} · ${model.tier}`));
      const stateBadge = el('span', 'router-state' + (isLoaded ? ' ok' : ''), label(runtimeState));
      stateBadge.dataset.status = runtimeState;
      const actionsWrap = el('div', 'ag-actions');
      const sw = el('button', 'lib-switch ag-conn' + (isEnabled ? ' on' : ''));
      sw.type = 'button';
      sw.dataset.action = 'toggle-model';
      sw.dataset.model = model.id;
      sw.setAttribute('role', 'switch');
      sw.setAttribute('aria-checked', String(isEnabled));
      sw.setAttribute('aria-label', name);
      sw.disabled = working || status.installation === 'downloading' || (!isEnabled && isSelected && !['ready', 'not_required'].includes(dependencies.state));
      sw.append(el('i'));
      sw.onclick = event => { event.stopPropagation(); toggleModel(model.id, isEnabled); };
      actionsWrap.append(sw);
      item.append(icon, who, stateBadge, actionsWrap);
      installedList.append(item);
    }
    wrap.append(head, installedList);
    return wrap;
  }
  function render() {
    const root = $('#localModelContent');
    root.replaceChildren();
    $('#localModelNote').textContent = t('Opening this page does not enable or download a model. Closing the page keeps 端云协同 running.');
    $('#localModelLifecycle').textContent = t('Enabled models stay loaded while 端云协同 runs. Your on/off choice is remembered at the next startup.');
    if (!managementToken) { root.append(el('p', 'hint', t('Management token required'))); return; }
    if (!data) { root.append(el('p', 'hint', t('Connection unavailable'))); return; }
    const status = data.status;
    const dependencies = status.dependencies || { state: 'unknown' };
    const choiceModelId = currentChoiceModelId(status);
    const choiceInstallation = choiceInstallationState(status, choiceModelId);
    const installedWrap = installedModelsSection(status, dependencies);
    if (installedWrap) root.append(installedWrap);
    root.append(deviceGuide());
    const choices = el('div', 'segs router-language'); choices.id = 'localModelChoices';
    chooseButtons(choices, data.catalog.models.map(model => [model.id, displayModelName(model)]), choiceModelId, async id => { await selectModel(id); });
    for (const button of choices.querySelectorAll('button')) button.disabled = working || status.installation === 'downloading';
    root.append(el('h3', 'router-label', t('Select a model')), choices);
    const list = el('div', 'list prefs');
    list.append(row('User intent', t(status.intent.enabled ? 'Enabled' : 'Disabled')), row('Installation', label(choiceInstallation)), row('Runtime state', label(status.runtime)), row('Queue occupancy', `${status.active} / ${status.waiting} (${status.queueLimit})`));
    const backend = status.backend || {};
    const verified = ['accelerated', 'hybrid'].includes(backend.state) && status.healthy && status.pid;
    const backendLabel = verified ? t(backend.state === 'hybrid' ? 'Hybrid CPU and GPU' : 'GPU acceleration') : t(backend.state === 'cpu' && status.healthy ? 'CPU inference' : 'Acceleration not verified');
    const backendRow = row('Inference backend', backendLabel + (verified ? ' · ' + String(backend.effective || '').toUpperCase() + ' · ' + (backend.deviceName || backend.device || '') + ' · ' + t('GPU layers: {loaded}/{total}', { loaded: backend.offloadedLayers, total: backend.totalLayers }) : ''));
    backendRow.id = 'localModelBackend'; list.append(backendRow);
    if (backend.fallbackReason) {
      const hint = backend.fallbackReason === 'accelerator_install_failed' ? 'GPU components could not be installed; CPU is available' : backend.fallbackReason === 'accelerator_not_installed' ? 'GPU components need installation or repair' : backend.state === 'cpu' && status.healthy ? 'GPU unavailable; using CPU' : 'Acceleration not verified';
      backendRow.append(el('p', 'local-backend-hint router-note', t(hint)));
    }
    const dependencyRow = row('Runtime dependencies', t(dependencyNames[dependencies.state] || 'Unknown') + (dependencies.version ? ' · ' + dependencies.version : ''));
    dependencyRow.id = 'localModelDependencies'; list.append(dependencyRow);
    const experience = status.performance?.state;
    const experienceKey = experience === 'acceptable' ? 'Response performance checked' : experience === 'slow' ? 'Slow local responses; automatic hybrid use is not recommended' : 'Performance pending confirmation';
    list.append(row('Local response experience', t(experienceKey)));
    const showDownloadRow = status.installation === 'downloading' || (choiceInstallation === 'failed' && Boolean(status.error || status.progress?.stage || status.progress?.reason));
    if (showDownloadRow) {
      const downloaded = Number(status.progress?.downloaded) || 0;
      const total = Number(status.progress?.total) || 0;
      const pct = total > 0 ? Math.min(100, Math.max(0, Math.round((downloaded / total) * 100))) : 0;
      const isFailed = status.installation === 'failed';
      const metaInfo = resolveProgressMeta(status, dependencies);
      const dlRow = el('div', 'row pref local-download-row' + (isFailed ? ' is-failed' : '')); dlRow.id = 'localDownloadProgress';
      const who = el('div', 'who');
      who.append(el('div', 'name', t(isFailed ? 'Download interrupted' : 'Downloading')));
      if (metaInfo.stageTitle) who.append(el('div', 'sub local-download-stage', metaInfo.stageTitle));
      if (metaInfo.hintText) who.append(el('div', 'sub local-download-hint', metaInfo.hintText));
      const wrap = el('div', 'local-progress-wrap');
      const meta = el('div', 'local-progress-meta');
      const text = total > 0 ? `${(downloaded / 1048576).toFixed(1)} / ${(total / 1048576).toFixed(1)} MiB (${pct}%)` : t(isFailed ? 'Failed' : 'Downloading');
      meta.append(el('span', 'router-state', text));
      const bar = el('progress', 'local-progress-bar' + (isFailed ? ' is-failed' : ''));
      bar.setAttribute('aria-label', t(isFailed ? 'Download interrupted' : 'Downloading'));
      if (total > 0) { bar.max = 100; bar.value = pct; }
      else if (isFailed) { bar.max = 100; bar.value = 0; }
      wrap.append(meta, bar);
      dlRow.append(who, wrap);
      list.append(dlRow);
    }
    root.append(list);
    if (['missing', 'failed', 'corrupt', 'architecture', 'version', 'layout'].includes(dependencies.state)) root.append(el('p', 'hint router-note', t('Enable the local model, then use Download and install to repair runtime dependencies.')));
    const buttons = el('div', 'request-choices local-model-actions');
    const assessment = device?.advice?.assessments?.find(item => item.id === choiceModelId);
    const resourceBlocked = assessment && assessment.manual !== 'passed';
    const installDisabled = resourceBlocked || !choiceModelId || status.installation === 'downloading' || (choiceInstallation === 'installed' && ['ready', 'not_required'].includes(dependencies.state) && !['accelerator_install_failed', 'accelerator_not_installed'].includes(status.backend?.fallbackReason));
    for (const [name, endpoint, disabled, handler] of [
      ['Download and install', 'install', installDisabled, installModel],
      ['Cancel download', 'cancel', status.installation !== 'downloading', () => action('cancel')],
      ['Check process health', 'health', !status.pid, () => action('health')],
      ['Check local response experience', 'experience', !status.pid, () => action('check')],
      ['Manual refresh', '', false, () => open()],
    ]) {
      const button = el('button', 'text local-model-action', t(name)); button.type = 'button'; button.dataset.action = endpoint || 'refresh'; button.disabled = working || disabled; button.onclick = handler; buttons.append(button);
    }
    root.append(buttons);
    if (status.error) root.append(el('p', 'editor-error local-status-error', statusErrorMessage(status)));
  }
  async function request(endpoint, body, signal) {
    const response = await managementFetch('/api/local-model' + (endpoint ? '/' + endpoint : ''), { method: endpoint ? 'POST' : 'GET', headers: { Authorization: 'Bearer ' + managementToken, ...(endpoint ? { 'Content-Type': 'application/json' } : {}) }, ...(endpoint ? { body: JSON.stringify(body || {}) } : {}), cache: 'no-store', credentials: 'omit', redirect: 'error', signal });
    const result = await response.json();
    if (!response.ok) {
      const code = result.error?.code;
      if (result.device) { device = result.device; render(); }
      if (String(code || '').startsWith('local_model_dependency_')) throw new Error(dependencyMessage(code));
      if (String(code || '').startsWith('local_model_install_')) throw new Error(statusErrorMessage({ error: code, progress: data?.status?.progress }));
      throw new Error(code === 'device_check_not_passed' ? 'Device check did not pass. Detect again and check memory, backend and free disk space.' : code === 'capability_model_unsupported' ? 'The selected model does not support this operation.' : code === 'capability_acceptance_suites_not_provided' ? 'Capability acceptance suites have not been provided.' : 'Local model operation failed. Refresh to inspect status.');
    }
    return result;
  }
  function isChanging(status) {
    return status?.installation === 'downloading' || ['loading', 'unloading'].includes(status?.runtime) || status?.dependencies?.state === 'preparing';
  }
  function canObserve() { return view === 'local-model' && managementToken && !document.hidden; }
  function stopObserving() {
    clearTimeout(retryTimer);
    stream?.abort(); stream = null;
    refreshController?.abort(); refreshController = null;
    readGeneration++;
  }
  function retryObservation() {
    if (!canObserve() || !isChanging(data?.status)) return;
    retryTimer = setTimeout(open, retryDelay);
    retryDelay = Math.min(retryDelay * 2, 30000);
  }
  async function refreshRedaction(status, force = false) {
    // Progress, queue occupancy and busy/idle transitions do not change privacy
    // readiness. Keep these out of the key so downloads do not query redaction.
    const key = JSON.stringify([status.intent, status.installation, status.dependencies, status.capabilities?.redaction, status.performance?.state, status.runtime === 'fault']);
    if (!force && key === redactionKey) return;
    await window.routerRedactionRefresh?.();
    redactionKey = key;
  }
  function watchStatus() {
    if (stream || !canObserve() || !isChanging(data?.status)) return;
    const controller = new AbortController(), current = connection;
    stream = controller;
    (async () => {
      let reader;
      try {
        // fetch keeps the management token in an Authorization header. Native
        // EventSource cannot set that header; tokens must never enter a URL.
        const response = await managementFetch('/api/local-model/events', { signal: controller.signal });
        if (!response.ok || !response.headers.get('Content-Type')?.startsWith('text/event-stream') || !response.body) throw new Error('Connection unavailable');
        reader = response.body.getReader();
        const decoder = new TextDecoder();
        let pending = '';
        while (!controller.signal.aborted) {
          const chunk = await reader.read();
          if (chunk.done) throw new Error('Connection unavailable');
          pending += decoder.decode(chunk.value, { stream: true });
          if (pending.length > 1024 * 1024) throw new Error('Connection unavailable');
          let end;
          while ((end = pending.indexOf('\n\n')) !== -1) {
            const frame = pending.slice(0, end); pending = pending.slice(end + 2);
            const json = frame.split('\n').filter(line => line.startsWith('data: ')).map(line => line.slice(6)).join('\n');
            if (!json) continue; // Heartbeats carry no state.
            const value = JSON.parse(json);
            if (value.schemaVersion !== 1 || !value.status) throw new Error('Connection unavailable');
            if (controller.signal.aborted || current !== connection || !canObserve()) return;
            retryDelay = 1000;
            if ($('#localModelError').textContent === t('Connection unavailable')) $('#localModelError').textContent = '';
            if (working) continue; // The action's final refresh owns its result.
            if (!isChanging(value.status) || value.status.intent?.selected !== data?.status?.intent?.selected) {
              // Terminal transitions need one full read to update installed
              // candidates as well as status, including a different model's install.
              void open();
              return;
            }
            data.status = value.status; render();
            await refreshRedaction(value.status);
          }
        }
      } catch {
        if (!controller.signal.aborted && current === connection && canObserve()) {
          $('#localModelError').textContent = t('Connection unavailable');
          retryObservation();
        }
      } finally {
        if (stream === controller) stream = null;
        if (reader) { await reader.cancel().catch(() => {}); reader.releaseLock(); }
      }
    })();
  }
  async function open() {
    stopObserving();
    if (!canObserve()) { render(); return; }
    const current = connection, generation = readGeneration, controller = new AbortController();
    refreshController = controller;
    try {
      const result = await request('', undefined, controller.signal);
      if (current !== connection || generation !== readGeneration) return;
      data = result;
      if ($('#localModelError').textContent === t('Connection unavailable')) $('#localModelError').textContent = '';
      if (!selectedModelId) selectedModelId = data?.status?.installingModel || data?.status?.intent?.selected || '';
      render();
      // One local probe per authenticated page session while no selected
      // installation is verified. Refreshes never loop on a failed probe.
      const skipDetection = window.routerLocalSkipDetectionOnce === true;
      window.routerLocalSkipDetectionOnce = false;
      if (!skipDetection && !detectionAttempted && !working && ['not_installed', 'canceled', 'failed'].includes(data.status.installation)) await detectDevice();
      if (current !== connection || generation !== readGeneration || !canObserve()) return;
      await refreshRedaction(data.status, true);
      if (generation === readGeneration) watchStatus();
    }
    catch {
      if (current === connection && generation === readGeneration && !controller.signal.aborted) {
        if (!data) render();
        $('#localModelError').textContent = t('Connection unavailable');
        retryObservation();
      }
    }
    finally { if (refreshController === controller) refreshController = null; }
  }
  async function detectDevice() {
    if (working || !managementToken) return;
    const current = connection;
    working = true; detecting = true; detectionAttempted = true; detectionFailed = false; device = null; render();
    try {
      const result = await request('detect');
      if (current !== connection) return;
      if (!result.device) throw new Error('missing_device_report');
      device = result.device;
      if (!selectedModelId && !data?.status?.intent?.selected && device.advice?.state === 'recommended') selectedModelId = device.advice.modelID;
    } catch { if (current === connection) detectionFailed = true; }
    finally { if (current === connection) { working = false; detecting = false; render(); } }
  }
  async function selectModel(id) {
    if (working || currentChoiceModelId(data?.status) === id) return;
    selectedModelId = id;
    $('#localModelError').textContent = '';
    if (data?.status?.intent?.enabled) {
      render();
      return;
    }
    if (data?.status?.intent?.selected === id) {
      render();
      return;
    }
    const current = connection;
    working = true; render();
    try {
      await request('select', { model: id });
    } catch (error) {
      if (current === connection) $('#localModelError').textContent = t(error.message);
    } finally {
      if (current === connection) { working = false; await open(); }
    }
  }
  async function installModel() {
    if (working) return;
    const current = connection;
    const choiceModelId = currentChoiceModelId(data?.status);
    working = true; render(); $('#localModelError').textContent = '';
    try {
      if (choiceModelId && !data?.status?.intent?.enabled && choiceModelId !== data?.status?.intent?.selected) {
        const selected = await request('select', { model: choiceModelId });
        if (selected?.status) data.status = selected.status;
      }
      if (!choiceModelId || String(choiceModelId).toLowerCase() === String(data?.status?.intent?.selected || '').toLowerCase()) {
        if (!data?.status?.intent?.enabled) {
          const enRes = await request('enable', {});
          if (enRes?.status) data.status = enRes.status;
        }
        await request('install', {});
      } else {
        await request('install', { model: choiceModelId });
      }
    } catch (error) {
      if (current === connection) $('#localModelError').textContent = t(error.message);
    } finally {
      if (current === connection) { working = false; await open(); }
    }
  }
  async function toggleModel(modelId, currentlyOn) {
    if (working) return;
    const current = connection;
    working = true; render(); $('#localModelError').textContent = '';
    let capabilityError = '';
    try {
      if (currentlyOn) {
        if (data?.status?.intent?.trial?.judgment || data?.status?.intent?.trial?.execution) {
          await request('trial/disable', {}).catch(() => {});
        }
        await request('disable', {});
      } else {
        if (data?.status?.intent?.selected !== modelId) {
          if (data?.status?.intent?.enabled) await request('disable', {});
          const selRes = await request('select', { model: modelId });
          if (selRes?.status) data.status = selRes.status;
        }
        selectedModelId = modelId;
        if (!data?.status?.intent?.enabled) {
          const enRes = await request('enable', {});
          if (enRes?.status) data.status = enRes.status;
        }
        const loadRes = await request('load', {});
        if (loadRes?.status) data.status = loadRes.status;
        try {
          const checkRes = await request('check', {});
          const st = checkRes?.status || data?.status;
          if (st?.performance && st.performance.state !== 'acceptable') {
            capabilityError = t(st.performance.state === 'slow' ? 'Slow local responses; automatic hybrid use is not recommended' : 'Performance is not confirmed; automatic hybrid use stays off.');
          } else if (st?.capabilities?.judgment?.trial && st?.capabilities?.judgment?.trialEvidence && st?.capabilities?.execution?.trial && st?.capabilities?.execution?.trialEvidence) {
            await request('trial/enable', {}).catch(() => {});
          } else if (st?.capabilities?.judgment?.state === 'unavailable' || st?.capabilities?.execution?.state === 'unavailable') {
            capabilityError = t('Capability acceptance suites have not been provided.');
          }
        } catch (checkErr) {
          capabilityError = t(checkErr.message);
        }
      }
    } catch (error) {
      if (current === connection) $('#localModelError').textContent = t(error.message);
    } finally {
      if (current === connection) {
        working = false;
        await open();
        if (capabilityError && !$('#localModelError').textContent) {
          $('#localModelError').textContent = capabilityError;
        }
      }
    }
  }
  async function action(endpoint, body) {
    if (working) return;
    const current = connection;
    working = true; render(); $('#localModelError').textContent = '';
    try { await request(endpoint, body); }
    catch (error) { if (current === connection) $('#localModelError').textContent = t(error.message); }
    finally { if (current === connection) { working = false; await open(); } }
  }
  window.routerLocalOpen = open;
  window.routerLocalRender = render;
  window.routerLocalDisconnected = () => { stopObserving(); connection++; data = null; device = null; working = false; detectionAttempted = false; detecting = false; detectionFailed = false; selectedModelId = ''; redactionKey = ''; render(); };
  document.addEventListener('visibilitychange', () => { if (document.hidden) stopObserving(); else if (canObserve()) void open(); });
  window.addEventListener('pagehide', stopObserving);
  window.addEventListener('pageshow', event => { if (event.persisted && canObserve()) void open(); });
  render();
})();
