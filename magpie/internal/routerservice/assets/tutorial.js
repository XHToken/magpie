'use strict';
(() => {
  // This guide keeps reading preferences only. It never changes model configuration.
  for (const [key, zh, tw, ja, de] of [
    ['Getting started', '新手入门', '新手入門', 'はじめに', 'Erste Schritte'],
    ['Start here', '从这里开始', '從這裡開始', 'ここから始める', 'Hier beginnen'],
    ['You can skip now and return from Usage guide.', '可以暂时跳过，稍后从“使用指南”继续。', '可以暫時略過，稍後從「使用指南」繼續。', '今はスキップし、後で「使用ガイド」から再開できます。', 'Jetzt überspringen und später über die Anleitung fortsetzen.'],
    ['Usage guide', '使用指南', '使用指南', '使用ガイド', 'Anleitung'],
    ['Tutorial chapters', '教学章节', '教學章節', 'ガイドの章', 'Kapitel der Anleitung'],
    ['Meet 端云协同', '认识 端云协同', '認識 端云协同', '端云协同を知る', '端云协同 kennenlernen'],
    ['Connect a provider', '连接供应商', '連接供應商', 'プロバイダーに接続', 'Anbieter verbinden'],
    ['Set up your app', '配置应用', '設定應用程式', 'アプリを設定', 'App einrichten'],
    ['Choose your local route', '选择本地路线', '選擇本機路線', 'ローカル利用を選ぶ', 'Lokale Nutzung wählen'],
    ['Install and try', '安装与试用', '安裝與試用', 'インストールと試用', 'Installieren und testen'],
    ['Your first request', '第一条请求', '第一筆請求', '最初のリクエスト', 'Die erste Anfrage'],
    ['Close guide', '关闭指南', '關閉指南', 'ガイドを閉じる', 'Anleitung schließen'],
    ['Skip for now', '暂时跳过', '暫時跳過', '今はスキップ', 'Vorerst überspringen'],
    ['Previous step', '上一步', '上一步', '前へ', 'Zurück'],
    ['Next step', '下一步', '下一步', '次へ', 'Weiter'],
    ['Start learning', '开始了解', '開始瞭解', 'ガイドを始める', 'Anleitung beginnen'],
    ['Skip local installation and continue', '跳过本地安装，继续', '略過本機安裝，繼續', 'ローカル導入をスキップして次へ', 'Lokale Installation überspringen'],
    ['Finish reading', '完成阅读', '完成閱讀', '読み終える', 'Lesen abschließen'],
    ['Continue reading', '继续阅读', '繼續閱讀', '続きを読む', 'Weiterlesen'],
    ['Read again', '重新阅读', '重新閱讀', 'もう一度読む', 'Erneut lesen'],
    ['Reading progress {current} / {total}', '阅读进度 {current} / {total}', '閱讀進度 {current} / {total}', '読書の進捗 {current} / {total}', 'Lesefortschritt {current} / {total}'],
    ['Chapter {number}: {name}', '第 {number} 章：{name}', '第 {number} 章：{name}', '第{number}章：{name}', 'Kapitel {number}: {name}'],
    ['Continue from “{chapter}”. Reading progress is separate from configuration.', '从“{chapter}”继续。阅读进度与实际配置分开记录。', '從「{chapter}」繼續。閱讀進度與實際設定分開記錄。', '「{chapter}」から続けます。読書の進捗と設定状況は別です。', 'Bei „{chapter}“ fortsetzen. Lesefortschritt und Konfiguration sind getrennt.'],
    ['Guide read. Check actual settings and request results to confirm readiness.', '指南已读完。是否可用，请以实际设置和请求结果为准。', '指南已讀完。是否可用，請以實際設定和請求結果為準。', 'ガイドを読み終えました。利用可否は実際の設定とリクエスト結果で確認してください。', 'Anleitung gelesen. Betriebsbereitschaft anhand der Einstellungen und Anfrageergebnisse prüfen.'],
    ['Start with your first AI connection', '从第一条 AI 连接开始', '從第一條 AI 連線開始', '最初のAI接続から始める', 'Mit der ersten KI-Verbindung beginnen'],
    ['Connect a cloud provider, bind it to your app, then add a local model if you need one. You can close this guide and continue at any time.', '先连接云端供应商，再为应用绑定模型；需要时再添加本地模型。你可以随时收起指南，稍后继续。', '先連接雲端供應商，再為應用程式綁定模型；需要時再加入本機模型。你可以隨時收起指南，稍後繼續。', 'クラウドプロバイダーに接続し、アプリにモデルを設定します。必要ならローカルモデルを追加します。ガイドはいつでも閉じて再開できます。', 'Cloud-Anbieter verbinden, der App ein Modell zuweisen und bei Bedarf ein lokales Modell ergänzen. Die Anleitung lässt sich jederzeit schließen und fortsetzen.'],
    ['Your app', '你的应用', '你的應用程式', '利用するアプリ', 'Deine App'],
    ['Cloud or local model', '云端或本机模型', '雲端或本機模型', 'クラウドまたはローカルモデル', 'Cloud- oder lokales Modell'],
    ['Request path: your app, 端云协同, cloud or local model', '请求路径：你的应用、端云协同、云端或本机模型', '請求路徑：你的應用程式、端云协同、雲端或本機模型', 'リクエスト経路：アプリ、端云协同、クラウドまたはローカルモデル', 'Anfrageweg: App, 端云协同, Cloud- oder lokales Modell'],
    ['Connect cloud first', '先接通云端', '先接通雲端', 'まずクラウドに接続', 'Zuerst Cloud verbinden'],
    ['Have your provider API key ready, then choose the cloud model for WorkBuddy or Loomy.', '准备好供应商 API 密钥，再选择 WorkBuddy 或 Loomy 使用的云端模型。', '準備好供應商 API 金鑰，再選擇 WorkBuddy 或 Loomy 使用的雲端模型。', 'プロバイダーのAPIキーを準備し、WorkBuddyまたはLoomyで使うクラウドモデルを選びます。', 'API-Schlüssel des Anbieters bereithalten und ein Cloud-Modell für WorkBuddy oder Loomy wählen.'],
    ['Local models are optional', '本地模型按需添加', '本機模型依需求加入', 'ローカルモデルは任意', 'Lokale Modelle sind optional'],
    ['Ordinary cloud use does not require a model download. Local Q4_K_M and Q8_0 routing and execution are experimental.', '普通云端使用不需要下载本地模型；本地 Q4_K_M 和 Q8_0 判断与执行属于实验性能力。', '一般雲端使用不需要下載本機模型；本機 Q4_K_M 和 Q8_0 判斷與執行屬於實驗性能力。', '通常のクラウド利用ではモデルのダウンロードは不要です。Q4_K_MとQ8_0による判断と実行は実験的機能です。', 'Für normale Cloud-Nutzung ist kein Modelldownload nötig. Lokales Routing und Ausführung mit Q4_K_M und Q8_0 sind experimentell.'],
    ['The page connects to local management automatically. If disconnected, check the service and use Refresh now. A management token is different from a provider API key.', '页面会自动连接本机管理。若显示断开，请检查服务并点击“立即刷新”。管理令牌与供应商 API 密钥用途不同。', '頁面會自動連接本機管理。若顯示中斷，請檢查服務並點擊「立即重新整理」。管理權杖與供應商 API 金鑰用途不同。', 'ページはローカル管理に自動接続します。切断時はサービスを確認し「今すぐ更新」を使ってください。管理トークンとプロバイダーのAPIキーは別です。', 'Die Seite verbindet sich automatisch mit der lokalen Verwaltung. Bei Trennung Dienst prüfen und „Jetzt aktualisieren“ nutzen. Verwaltungstoken und Anbieter-API-Schlüssel sind verschieden.'],
    ['Reading does not download models, enable trials or send model requests. Configuration actions remain yours.', '阅读不会下载模型、开启试用或发送模型请求，实际操作由你明确执行。', '閱讀不會下載模型、啟用試用或傳送模型請求，實際操作由你明確執行。', '読むだけではモデルのダウンロード、試用の有効化、モデルへのリクエストは行いません。各操作は自分で実行します。', 'Lesen lädt keine Modelle herunter, aktiviert keine Tests und sendet keine Modellanfragen. Jede Konfigurationsaktion wird ausdrücklich ausgeführt.'],
    ['Add your AI provider', '添加你的 AI 供应商', '新增你的 AI 供應商', 'AIプロバイダーを追加', 'KI-Anbieter hinzufügen'],
    ['A provider supplies the cloud service. In Providers, add the platform you use and save its connection details. Choose the app model in the next chapter.', '供应商提供云端服务。在“供应商”中添加你使用的平台并保存连接信息，下一章再为应用选择模型。', '供應商提供雲端服務。在「供應商」中新增你使用的平台並儲存連線資訊，下一章再為應用程式選擇模型。', 'プロバイダーがクラウドサービスを提供します。「プロバイダー」で利用先を追加して接続情報を保存し、次の章でアプリのモデルを選びます。', 'Der Anbieter stellt den Cloud-Dienst bereit. Unter Anbieter die Plattform hinzufügen und Verbindungsdaten speichern. Das App-Modell folgt im nächsten Kapitel.'],
    ['Provider and platform', '供应商与平台', '供應商與平台', 'プロバイダーとプラットフォーム', 'Anbieter und Plattform'],
    ['Choose your existing provider and confirm the platform or region that issued your key.', '选择你已开通的供应商，并核对密钥所属平台或地区。', '選擇你已開通的供應商，並確認金鑰所屬平台或地區。', '契約済みのプロバイダーを選び、キーが発行されたプラットフォームや地域を確認します。', 'Vorhandenen Anbieter wählen und Plattform oder Region des Schlüssels prüfen.'],
    ['API protocol', 'API 协议', 'API 協定', 'APIプロトコル', 'API-Protokoll'],
    ['Use the protocol supported by that provider. The model name alone does not determine the protocol.', '按供应商支持情况选择协议，不能只根据模型名称判断。', '依供應商支援情況選擇協定，不能只依模型名稱判斷。', 'プロバイダーが対応するプロトコルを選びます。モデル名だけでは判断できません。', 'Ein vom Anbieter unterstütztes Protokoll wählen. Der Modellname allein bestimmt es nicht.'],
    ['API base address', 'API 基础地址', 'API 基礎位址', 'APIベースアドレス', 'API-Basisadresse'],
    ['Copy the base address from the provider console, without /chat/completions or /messages.', '复制供应商控制台给出的基础地址，不附加 /chat/completions 或 /messages。', '複製供應商控制台提供的基礎位址，不附加 /chat/completions 或 /messages。', 'プロバイダーの管理画面からベースアドレスをコピーします。/chat/completionsや/messagesは付けません。', 'Basisadresse aus der Anbieter-Konsole kopieren, ohne /chat/completions oder /messages.'],
    ['Provider API key', '供应商 API 密钥', '供應商 API 金鑰', 'プロバイダーのAPIキー', 'Anbieter-API-Schlüssel'],
    ['Use an API key issued by this provider. Never paste the local management token here.', '填写此供应商签发的 API Key，不要填入本机管理令牌。', '填寫此供應商簽發的 API Key，不要填入本機管理權杖。', 'このプロバイダーが発行したAPIキーを使います。ローカル管理トークンは入力しません。', 'Einen von diesem Anbieter ausgestellten API-Schlüssel verwenden. Hier kein lokales Verwaltungstoken eingeben.'],
    ['Choose app instructions', '选择应用教学', '選擇應用程式教學', 'アプリの説明を選択', 'App-Anleitung wählen'],
    ['Restart the app and start a new session', '重启应用并新建会话', '重新啟動應用程式並建立新會話', 'アプリを再起動し新しいセッションを開始', 'App neu starten und neue Sitzung öffnen'],
    ['Finish existing tasks, restart WorkBuddy and choose the 端云协同 model. Send your first request in a new session.', '先结束已有任务，再重启 WorkBuddy 并选择 端云协同 对应模型，在新会话中发送第一条请求。', '先結束現有任務，再重新啟動 WorkBuddy 並選擇 端云协同 對應模型，在新會話中傳送第一筆請求。', '実行中の作業を終えてWorkBuddyを再起動し、端云协同のモデルを選びます。新しいセッションから最初のリクエストを送ってください。', 'Laufende Aufgaben beenden, WorkBuddy neu starten und das 端云协同-Modell wählen. Die erste Anfrage in einer neuen Sitzung senden.'],
    ['Restart Loomy, choose 端云协同 and start a new session. Existing sessions and project-level .opencode settings may keep a different model or provider.', '重启 Loomy，选择 端云协同 并新建会话。已有会话及项目级 .opencode 设置可能继续使用其他模型或供应商。', '重新啟動 Loomy，選擇 端云协同 並建立新會話。現有會話及專案層級 .opencode 設定可能繼續使用其他模型或供應商。', 'Loomyを再起動し、端云协同を選んで新しいセッションを開始します。既存セッションやプロジェクトの.opencode設定は別のモデルを使う場合があります。', 'Loomy neu starten, 端云协同 wählen und eine neue Sitzung öffnen. Bestehende Sitzungen und .opencode-Projekteinstellungen können andere Modelle oder Anbieter verwenden.'],
    ['Do I need a local model?', '我需要本地模型吗？', '我需要本機模型嗎？', 'ローカルモデルは必要？', 'Brauche ich ein lokales Modell?'],
    ['A local model can judge and answer suitable requests on this computer. Successful local completion may reduce cloud calls; speed, quality and actual savings depend on the device and task.', '本地模型可在本机判断并回答适合的请求。成功在本机完成时，可能减少云端调用；速度、质量与实际节省取决于设备和任务。', '本機模型可在本機判斷並回答適合的請求。成功在本機完成時，可能減少雲端呼叫；速度、品質與實際節省取決於裝置和任務。', 'ローカルモデルは適したリクエストをこの端末で判断・回答できます。ローカルで完了すればクラウド呼び出しを減らせる場合がありますが、速度・品質・節約量は端末と作業次第です。', 'Ein lokales Modell kann passende Anfragen auf diesem Computer beurteilen und beantworten. Erfolgreiche lokale Ausführung kann Cloud-Aufrufe verringern; Tempo, Qualität und Ersparnis hängen von Gerät und Aufgabe ab.'],
    ['Spark-X2.6-4B · Q4_K_M / Q8_0', 'Spark-X2.6-4B · Q4_K_M / Q8_0', 'Spark-X2.6-4B · Q4_K_M / Q8_0', 'Spark-X2.6-4B · Q4_K_M / Q8_0', 'Spark-X2.6-4B · Q4_K_M / Q8_0'],
    ['Both tiers use the same installation and trial flow', '两个档位使用相同的安装与试用流程', '兩個級別使用相同的安裝與試用流程', 'どちらも同じ導入と試用の手順を使います', 'Beide Stufen verwenden denselben Installations- und Testablauf'],
    ['Experimental', '实验性', '實驗性', '実験的', 'Experimentell'],
    ['Choose Q4_K_M or Q8_0 based on available memory, disk and supported runtime, then confirm with device detection. You can keep using cloud without installing a model.', '按可用内存、磁盘和受支持的运行后端选择 Q4_K_M 或 Q8_0，再用设备检测确认。不安装也可继续使用云端。', '依可用記憶體、磁碟和受支援的執行後端選擇 Q4_K_M 或 Q8_0，再用裝置偵測確認。不安裝也可繼續使用雲端。', '利用可能なメモリ、ディスク、対応ランタイムに応じてQ4_K_MかQ8_0を選び、端末検出で確認します。導入せずクラウドを使い続けることもできます。', 'Q4_K_M oder Q8_0 nach freiem Speicher, Festplattenplatz und unterstützter Laufzeit wählen und per Geräteprüfung bestätigen. Cloud-Nutzung bleibt ohne Installation möglich.'],
    ['Choose a learning route', '选择学习路线', '選擇學習路線', '学習ルートを選択', 'Lernweg wählen'],
    ['Learn local installation and trial', '了解本地安装与试用', '瞭解本機安裝與試用', 'ローカル導入と試用を学ぶ', 'Lokale Installation und Test kennenlernen'],
    ['Use cloud first', '先使用云端', '先使用雲端', 'まずクラウドを使う', 'Zunächst Cloud nutzen'],
    ["Local trial and redaction have separate setup and readiness checks. Enabling local routing does not automatically redact cloud requests; cloud traffic still reaches your selected provider.","本地试用与脱敏需要分别配置并确认就绪。开启本地路由不会自动脱敏云端请求，云端流量仍发往所选供应商。","本機試用與去識別化需要分別設定並確認就緒。開啟本機路由不會自動去識別化雲端請求，雲端流量仍傳送至所選供應商。","ローカル試用と匿名化は別々に設定し利用可否を確認します。ローカルルーティングだけではクラウドリクエストは匿名化されず、選んだプロバイダーに送られます。","Lokaler Test und Anonymisierung erfordern eigene Einrichtung und Bereitschaftsprüfung. Lokales Routing anonymisiert Cloud-Anfragen nicht automatisch; sie gehen weiterhin an den gewählten Anbieter."],
    ['Understand local trial limits', '了解本地试用限制', '瞭解本機試用限制', 'ローカル試用の制限', 'Grenzen des lokalen Tests verstehen'],
    ['Engineering checks are not quality acceptance. Local answers, cloud-routing decisions and answers after tool calls may fail or time out. Use actual request records to judge results.', '工程检查不等于质量验收。本地回答、转云判断和工具后续回答可能失败或超时；请以真实请求记录确认结果。', '工程檢查不等於品質驗收。本機回答、轉雲判斷和工具後續回答可能失敗或逾時；請以真實請求紀錄確認結果。', '技術的な検査は品質の受け入れ検証ではありません。ローカルの回答、クラウドへの判断、ツール後の回答は失敗やタイムアウトがあり得ます。実際のリクエスト記録で確認してください。', 'Technische Prüfungen sind keine Qualitätsabnahme. Lokale Antworten, Cloud-Entscheidungen und Antworten nach Tool-Aufrufen können fehlschlagen oder auslaufen. Tatsächliche Anfrageprotokolle prüfen.'],
    ['Detect the device', '检测设备', '偵測裝置', '端末を検出', 'Gerät prüfen'],
    ['Check memory, disk and runtime conditions. A recommendation is not a download or a quality guarantee.', '确认内存、磁盘与运行后端条件。推荐结果不会下载模型，也不是质量保证。', '確認記憶體、磁碟與執行後端條件。建議結果不會下載模型，也不是品質保證。', 'メモリ、ディスク、ランタイム条件を確認します。推奨結果はダウンロードや品質保証ではありません。', 'Speicher, Festplatte und Laufzeit prüfen. Eine Empfehlung lädt nichts herunter und ist keine Qualitätsgarantie.'],
    ['Select Q4_K_M or Q8_0', '选择 Q4_K_M 或 Q8_0', '選擇 Q4_K_M 或 Q8_0', 'Q4_K_MかQ8_0を選択', 'Q4_K_M oder Q8_0 auswählen'],
    ['Choose a fixed Q4_K_M or Q8_0 model. Both use the same download, configuration and capability-check flow.', '选择固定的 Q4_K_M 或 Q8_0 模型。两者使用相同的下载、配置与能力检查流程。', '選擇固定的 Q4_K_M 或 Q8_0 模型。兩者使用相同的下載、設定與能力檢查流程。', '固定のQ4_K_MかQ8_0モデルを選びます。どちらも同じダウンロード、設定、機能確認の手順を使います。', 'Ein festes Q4_K_M- oder Q8_0-Modell auswählen. Beide verwenden denselben Ablauf für Download, Konfiguration und Fähigkeitsprüfung.'],
    ['Download and install the model', '下载并安装模型', '下載並安裝模型', 'モデルをダウンロードして導入', 'Modell herunterladen und installieren'],
    ['Skip installation for now', '暂时跳过安装', '暫時略過安裝', '今は導入をスキップ', 'Installation vorerst überspringen'],
    ['Send a request and see where it went', '发一条请求，看看它去了哪里', '傳送一筆請求，看看它去了哪裡', 'リクエストを送り、実行先を確認', 'Anfrage senden und Ziel prüfen'],
    ['After configuration, restart the connected app, choose 端云协同 and open a new session. Send a simple request there, then return here to inspect actual results.', '完成配置后，重启已接入的应用，选择 端云协同 并新建会话。先在应用中发送一条简单请求，再回来查看实际结果。', '完成設定後，重新啟動已接入的應用程式，選擇 端云协同 並建立新會話。先在應用程式中傳送一筆簡單請求，再回來查看實際結果。', '設定後、接続したアプリを再起動して端云协同を選び、新しいセッションを開きます。簡単なリクエストを送り、ここで実際の結果を確認してください。', 'Nach der Einrichtung die verbundene App neu starten, 端云协同 wählen und eine neue Sitzung öffnen. Eine einfache Anfrage senden und hier das tatsächliche Ergebnis prüfen.'],
    ['Try: briefly explain what you can help me with.', '可以试试：请用一句话介绍你能帮我做什么。', '可以試試：請用一句話介紹你能幫我做什麼。', '例：何を手伝えるか、一文で教えてください。', 'Beispiel: Erkläre in einem Satz, wobei du mir helfen kannst.'],
    ['Usage → Requests', '用量 → 请求', '用量 → 請求', '使用量 → リクエスト', 'Nutzung → Anfragen'],
    ['Open the request detail for status, actual tokens, latency and internal calls. An error is a result to investigate, not a successful local answer.', '打开请求详情，查看状态、实际 Token、耗时与内部调用。错误需要排查，不能计作成功的本地回答。', '開啟請求詳情，查看狀態、實際 Token、耗時與內部呼叫。錯誤需要排查，不能計作成功的本機回答。', '詳細で状態、実際のトークン、所要時間、内部呼び出しを確認します。エラーは調査対象であり、ローカル回答の成功ではありません。', 'Anfragedetails für Status, tatsächliche Tokens, Dauer und interne Aufrufe öffnen. Ein Fehler muss untersucht werden und ist keine erfolgreiche lokale Antwort.'],
    ['Routing', '路由', '路由', 'ルーティング', 'Routing'],
    ['Usage → Overview', '用量 → 概览', '用量 → 概覽', '使用量 → 概要', 'Nutzung → Übersicht'],
    ['Review aggregates within the retained window. Only requests through 端云协同 are counted. Unreported tokens remain unknown; reference prices and savings are estimates, not bills.', '查看保留窗口内汇总。只统计经过 端云协同 的请求；未报告 Token 为未知，参考价格与节省金额属于估算，不是账单。', '查看保留視窗內彙總。只統計經過 端云协同 的請求；未回報 Token 為未知，參考價格與節省金額屬於估算，不是帳單。', '保持期間内の集計を確認します。端云协同を通ったリクエストだけが対象です。未報告のトークンは不明のままで、参考価格や節約額は請求額ではなく推定です。', 'Summen im Aufbewahrungszeitraum ansehen. Gezählt werden nur Anfragen über 端云协同. Nicht gemeldete Tokens bleiben unbekannt; Referenzpreise und Ersparnis sind Schätzungen, keine Rechnungen.'],
    ['Finishing marks this guide as read only. Provider setup, host connection and local trial readiness must be checked in their own pages and real requests.', '完成阅读只标记指南已读。供应商配置、宿主接入与本地试用是否可用，仍需在对应页面及真实请求中验证。', '完成閱讀只標記指南已讀。供應商設定、宿主接入與本機試用是否可用，仍需在對應頁面及真實請求中驗證。', '読み終える操作はガイドの既読だけを記録します。プロバイダー設定、ホスト接続、ローカル試用の可否は各ページと実際のリクエストで確認します。', 'Abschließen markiert nur die Anleitung als gelesen. Anbieterkonfiguration, Host-Verbindung und lokale Testbereitschaft müssen auf ihren Seiten und mit echten Anfragen geprüft werden.'],
    ['Go to Agents', '前往 Agent', '前往 Agent', 'Agentを開く', 'Agents öffnen'],
    ['Go to Providers', '前往供应商', '前往供應商', 'プロバイダーを開く', 'Anbieter öffnen'],
    ['Go to Local model', '前往本地模型', '前往本機模型', 'ローカルモデルを開く', 'Lokales Modell öffnen'],
    ['Go to Requests', '前往请求记录', '前往請求紀錄', 'リクエスト記録を開く', 'Anfrageverlauf öffnen'],
    ['Use routing groups', '使用路由组', '使用路由組', 'ルーティンググループを使う', 'Routing-Gruppen nutzen'],
    ['Redact and inspect', '脱敏与核验', '去識別化與核驗', '匿名化と確認', 'Anonymisieren und prüfen'],
    ['Troubleshoot and restore', '排查与还原', '排查與還原', '問題解決と復元', 'Fehler beheben und wiederherstellen'],
    ['Select models, then connect your app', '选择模型，再接入应用', '選擇模型，再接入應用程式', 'モデルを選びアプリに接続', 'Modelle wählen und App verbinden'],
    ['Install and turn on a local model', '安装并开启本地模型', '安裝並開啟本機模型', 'ローカルモデルを導入して有効化', 'Lokales Modell installieren und einschalten'],
    ['One group, several model choices', '一个路由组，多个模型协作', '一個路由組，多個模型協作', '複数のモデルを一つのグループに', 'Mehrere Modelle in einer Gruppe'],
    ['Enable redaction and check the result', '开启脱敏，并检查实际结果', '開啟去識別化，並檢查實際結果', '匿名化を有効にして結果を確認', 'Anonymisierung aktivieren und Ergebnis prüfen'],
    ['Find the cause and restore your app', '定位问题，安全还原应用', '定位問題，安全還原應用程式', '原因を調べてアプリを復元', 'Ursache finden und App wiederherstellen'],
    ['Go to Routing', '前往路由', '前往路由', 'ルーティングを開く', 'Routing öffnen'],
    ['Go to Redaction records', '前往脱敏记录', '前往去識別化紀錄', '匿名化記録を開く', 'Anonymisierungsprotokoll öffnen'],
    ['Manage the service and startup', '管理服务与开机自启', '管理服務與開機自啟', 'サービスと自動起動を管理', 'Dienst und Autostart verwalten'],
    ['Check redaction startup permission', '确认脱敏启动授权', '確認去識別化啟動授權', '匿名化の起動許可を確認', 'Startfreigabe für Anonymisierung prüfen'],
    ['Redaction startup permission is enabled by default. In Settings → Service management, check Experimental redaction at startup. If disabled, stop 端云协同, then start the same installed EXE with service start --experimental-redaction. The group’s first local member must match the enabled model. With group redaction on, local preparation precedes the cloud answer.', '服务默认允许实验性脱敏。在“设置 → 服务管理”查看“启动时实验性脱敏”。未开启时先停止 端云协同，再用同一安装位置的 EXE 执行 service start --experimental-redaction。组内第一个本地成员必须与当前开启模型一致。组脱敏开启后，本地负责准备脱敏内容，再由云端回答。', '服務預設允許實驗性去識別化。在「設定 → 服務管理」查看啟動時實驗性去識別化。未開啟時先停止 端云协同，再用同一安裝位置的 EXE 執行 service start --experimental-redaction。組內第一個本機成員必須與目前開啟模型一致。組去識別化開啟後，本機準備內容，再由雲端回答。', '起動時の実験的匿名化は既定で有効です。設定のサービス管理で起動時の実験的匿名化を確認します。無効なら端云协同を停止し、同じインストール済みEXEでservice start --experimental-redactionを実行します。最初のローカルメンバーは有効なモデルと一致させます。グループ匿名化ではローカルで準備しクラウドが回答します。', 'Die experimentelle Anonymisierung beim Start ist standardmäßig aktiviert. Unter Einstellungen → Dienstverwaltung die experimentelle Anonymisierung beim Start prüfen. Falls deaktiviert, 端云协同 stoppen und dieselbe installierte EXE mit service start --experimental-redaction starten. Das erste lokale Mitglied muss dem aktiven Modell entsprechen. Bei Gruppenanonymisierung bereitet das lokale Modell vor; die Cloud antwortet.'],
    ['Open Settings → Service management to inspect the running program and login startup setting. Before stopping or restarting 端云协同, acknowledge that the page will disconnect. Stopping keeps host connections configured; use your usual launcher to start again. After a restart, the page reconnects automatically.', '打开“设置 → 服务管理”，查看运行程序及登录自启设置。停止或重启 端云协同 前，勾选确认页面将断开连接。停止后宿主接入配置保留；需要时通过原来的启动入口重新启动。重启后页面会自动恢复连接。', '開啟「設定 → 服務管理」，查看執行程式及登入自啟設定。停止或重新啟動 端云协同 前，勾選確認頁面將中斷連線。停止後宿主接入設定保留；需要時透過原來的啟動入口重新啟動。重新啟動後頁面會自動恢復連線。', '設定のサービス管理で実行中のプログラムとログイン時の自動起動を確認します。停止・再起動前にページが切断されることを確認します。停止後もホスト設定は残ります。通常の起動方法で起動できます。再起動後、ページは自動的に再接続します。', 'Unter Einstellungen → Dienstverwaltung Programm und Autostart prüfen. Vor Stoppen oder Neustarten die Trennung der Seite bestätigen. Host-Verbindungen bleiben erhalten; 端云协同 bei Bedarf wie gewohnt starten. Nach einem Neustart verbindet sich die Seite automatisch.'],
    ['Save the provider, then check its model list. A failed model-list fetch does not undo the save. A preset or cached list is only a candidate list; confirm access with a real request from your app.', '保存供应商后查看模型列表。获取列表失败不会撤销已保存的配置。预置或缓存列表只是候选项；请从应用发送真实请求，确认账号有权限使用。', '儲存供應商後查看模型清單。取得清單失敗不會撤銷已儲存的設定。預設或快取清單只是候選項；請從應用程式傳送真實請求，確認帳號有使用權限。', 'プロバイダーを保存しモデル一覧を確認します。一覧取得の失敗で保存は取り消されません。プリセットやキャッシュは候補なので、アプリから実際にリクエストして権限を確認します。', 'Anbieter speichern und Modellliste prüfen. Ein fehlgeschlagener Abruf macht das Speichern nicht rückgängig. Vorgaben und Cache sind nur Kandidaten; den Zugriff mit einer echten App-Anfrage prüfen.'],
    ['In Agents, find your app, select its models and turn on its connection switch. Start with one cloud model; add more models or groups after the first request works.', '在 Agent 页找到你的应用，选择模型并开启连接开关。建议先接通一个云端模型，第一条请求成功后再增加模型或路由组。', '在 Agent 頁找到你的應用程式，選擇模型並開啟連線開關。建議先接通一個雲端模型，第一筆請求成功後再增加模型或路由組。', 'Agentページでアプリを探し、モデルを選んで接続スイッチをオンにします。まずクラウドモデルを一つ接続し、最初のリクエスト成功後にモデルやグループを追加します。', 'Unter Agents die App suchen, Modelle wählen und den Verbindungsschalter einschalten. Mit einem Cloud-Modell beginnen und nach der ersten erfolgreichen Anfrage weitere Modelle oder Gruppen ergänzen.'],
    ['Pick models and set the default', '挑选模型并设置默认项', '挑選模型並設定預設項', 'モデルと既定値を選ぶ', 'Modelle und Standard wählen'],
    ['Open “Choose models”, search by provider or model and tick the models you need. Use ↑ / ↓ to move the desired default to #1. Changes save automatically. For an unlisted model, add provider/model using the saved provider ID and exact model ID.', '打开“选择路由组或模型”，按供应商或模型搜索并勾选。用 ↑ / ↓ 把默认模型移到 #1，改动会自动保存。列表外的模型可用 provider/model 添加，其中 provider 为已保存的供应商 ID，model 为准确的模型 ID。', '開啟「選擇路由組或模型」，依供應商或模型搜尋並勾選。用 ↑ / ↓ 把預設模型移到 #1，變更會自動儲存。清單外的模型可用 provider/model 新增，其中 provider 為已儲存的供應商 ID，model 為準確的模型 ID。', 'モデル選択を開き、プロバイダーやモデルを検索してチェックします。↑ / ↓で既定モデルを#1に移動します。変更は自動保存されます。一覧にないモデルは保存済みプロバイダーIDと正確なモデルIDを使いprovider/modelで追加します。', 'Modellauswahl öffnen, nach Anbieter oder Modell suchen und auswählen. Mit ↑ / ↓ das Standardmodell auf #1 setzen. Änderungen werden automatisch gespeichert. Fehlende Modelle als provider/model mit gespeicherter Anbieter-ID und genauer Modell-ID ergänzen.'],
    ['Turn on the connection switch', '开启应用连接开关', '開啟應用程式連線開關', '接続スイッチをオンにする', 'Verbindungsschalter einschalten'],
    ['Finish running tasks and close the app before changing its connection. Turn on the switch in its Agent row and wait for Connected. 端云协同 writes the selected model list to the detected host configuration; this does not verify cloud generation access.', '修改连接前先结束任务并关闭应用。开启对应 Agent 行的开关，等待显示“已连接”。端云协同 会将所选模型列表写入检测到的宿主配置；连接成功不代表云端生成权限已验证。', '修改連線前先結束任務並關閉應用程式。開啟對應 Agent 列的開關，等待顯示「已連線」。端云协同 會將所選模型清單寫入偵測到的宿主設定；連線成功不代表雲端生成權限已驗證。', '接続変更前に作業を終えてアプリを閉じます。該当Agentのスイッチをオンにして接続済みを待ちます。選択したモデル一覧がホスト設定に書き込まれますが、クラウド生成権限の検証ではありません。', 'Vor Änderungen Aufgaben beenden und App schließen. Schalter in der Agent-Zeile einschalten und auf Verbunden warten. 端云协同 schreibt die Modellauswahl in die erkannte Host-Konfiguration; Cloud-Generierungsrechte werden dabei nicht geprüft.'],
    ['Only selected models appear in the app. Open Details to review the model list and new-session default. Changes while connected also update the host configuration; fully quit and reopen the app to reload it.', '应用只会列出这里勾选的模型。展开“详情”可查看模型列表和“新会话默认”。连接中修改模型也会更新宿主配置，改完请完全退出并重开应用。', '應用程式只會列出這裡勾選的模型。展開「詳情」可查看模型清單和新工作階段預設。連線中修改模型也會更新宿主設定，改完請完全退出並重開應用程式。', 'アプリには選択したモデルだけが表示されます。詳細で一覧と新規セッションの既定値を確認できます。接続中の変更もホスト設定を更新するので、アプリを完全終了して開き直します。', 'Die App zeigt nur ausgewählte Modelle. Unter Details Modellliste und Standard für neue Sitzungen prüfen. Änderungen bei bestehender Verbindung aktualisieren auch die Host-Konfiguration; App danach vollständig beenden und neu öffnen.'],
    ['Loomy uses the provider credentials stored in 端云协同. If connection is rejected because of a host extension, inspect the reported extension conflict before retrying.', 'Loomy 使用 端云协同 中保存的供应商凭证。如果因宿主扩展而拒绝接入，请先检查提示的扩展冲突再重试。', 'Loomy 使用 端云协同 中儲存的供應商憑證。如果因宿主擴充功能而拒絕接入，請先檢查提示的擴充功能衝突再重試。', 'Loomyは端云协同に保存したプロバイダー認証情報を使います。ホスト拡張機能により接続が拒否された場合、表示された競合を確認してから再試行します。', 'Loomy nutzt die in 端云协同 gespeicherten Anbieter-Zugangsdaten. Wird die Verbindung wegen einer Host-Erweiterung abgelehnt, zuerst den gemeldeten Konflikt prüfen.'],
    ['Use Local model to perform these actions in order. Installation prepares files; the installed-model switch starts or stops local use.', '在“本地模型”页依次操作。安装负责准备文件，已安装模型的开关负责启动或停止本地使用。', '在「本機模型」頁依序操作。安裝負責準備檔案，已安裝模型的開關負責啟動或停止本機使用。', 'ローカルモデルページで順番に操作します。インストールはファイルの準備で、導入済みモデルのスイッチがローカル利用を開始・停止します。', 'Unter Lokales Modell der Reihe nach vorgehen. Die Installation bereitet Dateien vor; der Schalter des installierten Modells startet oder stoppt die lokale Nutzung.'],
    ['Click “Download and install” to authorize installation of the selected model and dependencies. Follow download progress and integrity checks; use Cancel download to pause, then retry installation after an interruption.', '点击“下载并安装”，授权安装所选模型及依赖。查看下载进度并等待完整性校验；可用“取消下载”中断，之后重新点击安装继续。', '點擊「下載並安裝」，授權安裝所選模型及相依套件。查看下載進度並等待完整性驗證；可用「取消下載」中斷，之後重新點擊安裝繼續。', 'ダウンロードとインストールを押し、モデルと依存関係の導入を許可します。進捗と整合性検証を確認します。中断はキャンセルを使い、後で再度インストールします。', 'Download und Installation anklicken, um Modell und Abhängigkeiten zu installieren. Fortschritt und Integritätsprüfung abwarten. Zum Unterbrechen Download abbrechen und später die Installation erneut starten.'],
    ['Turn on the installed model', '开启已安装模型', '開啟已安裝模型', '導入済みモデルをオンにする', 'Installiertes Modell einschalten'],
    ['In “Configured on-device models”, turn on the model switch. 端云协同 selects and loads it, runs local capability checks and attempts to enable the experimental trial when checks pass. Read any errors and current runtime state before using it.', '在“已配置的端侧模型”中开启模型开关。端云协同 会选择并加载模型、执行本地能力检查，检查通过时尝试开启实验试用。使用前请查看当前运行状态及错误提示。', '在「已設定的端側模型」中開啟模型開關。端云协同 會選擇並載入模型、執行本機能力檢查，通過時嘗試開啟實驗試用。使用前請查看目前執行狀態及錯誤提示。', '設定済み端末内モデルのスイッチをオンにします。端云协同が選択・ロード・機能検査を行い、合格すると実験的な試用の有効化を試みます。使用前に現在の状態とエラーを確認します。', 'Unter Konfigurierte lokale Modelle den Schalter einschalten. 端云协同 wählt und lädt das Modell, prüft Fähigkeiten und versucht nach bestandener Prüfung den experimentellen Test zu aktivieren. Vor Nutzung Zustand und Fehler lesen.'],
    ['Check the backend and connect the model', '检查后端并接入模型', '檢查後端並接入模型', 'バックエンドを確認して接続', 'Backend prüfen und Modell verbinden'],
    ['Inspect Inference backend and Check process health. A detected GPU does not prove acceleration: check the effective backend and loaded layers. CPU fallback can still run. In Agents, select local/<model-id> or a routing group containing it, then restart the app.', '查看“推理后端”，点击“检测进程健康”。检测到 GPU 不等于已加速，请确认实际后端和卸载层数；回退 CPU 后仍可运行。在 Agent 中选择 local/<model-id> 或包含它的路由组，再重启应用。', '查看「推理後端」，點擊「檢查程序健康」。偵測到 GPU 不等於已加速，請確認實際後端和卸載層數；退回 CPU 後仍可執行。在 Agent 中選擇 local/<model-id> 或包含它的路由組，再重新啟動應用程式。', '推論バックエンドとプロセスの健全性を確認します。GPU検出だけでは加速の証拠にならないため、実効バックエンドとレイヤー数を見ます。CPUへの切替でも動作します。Agentでlocal/<model-id>かそのグループを選び、アプリを再起動します。', 'Inferenz-Backend und Prozesszustand prüfen. Eine erkannte GPU beweist keine Beschleunigung; tatsächliches Backend und ausgelagerte Schichten prüfen. CPU-Fallback kann weiterarbeiten. Unter Agents local/<model-id> oder eine entsprechende Gruppe wählen und App neu starten.'],
    ['Enabled models stay loaded while 端云协同 runs and the on/off choice is remembered on restart. Closing the page does not stop the service. To release local resources, turn off the installed model switch.', '端云协同 运行期间，已开启模型会保持加载，重启后会记住开关选择。关闭页面不会停止服务；需要释放本地资源时，请关闭已安装模型的开关。', '端云协同 執行期間，已開啟模型會保持載入，重新啟動後會記住開關選擇。關閉頁面不會停止服務；需要釋放本機資源時，請關閉已安裝模型的開關。', '有効なモデルは端云协同実行中ロード状態を保ち、スイッチの選択は再起動後も記憶されます。ページを閉じてもサービスは停止しません。資源を解放するにはモデルのスイッチをオフにします。', 'Aktivierte Modelle bleiben während des 端云协同-Betriebs geladen; die Schalterstellung wird beim Neustart berücksichtigt. Das Schließen der Seite stoppt den Dienst nicht. Zum Freigeben lokaler Ressourcen den Modellschalter ausschalten.'],
    ['Request details and fallback attempts', '请求详情与兜底尝试', '請求詳情與備援嘗試', 'リクエスト詳細と切替の試行', 'Anfragedetails und Fallback-Versuche'],
    ['Click a request row to inspect the actual destination, model, failure stage and internal calls. For a routing group, expand its attempts to see which member failed and which eventually answered. The group name alone does not identify the executing model.', '点击请求行，查看实际去向、模型、失败阶段与内部调用。路由组请求可展开尝试记录，确认哪个成员失败、最终由谁回答；仅看路由组名称无法确认实际执行模型。', '點擊請求列，查看實際去向、模型、失敗階段與內部呼叫。路由組請求可展開嘗試紀錄，確認哪個成員失敗、最終由誰回答；僅看路由組名稱無法確認實際執行模型。', '行をクリックし実行先、モデル、失敗段階、内部呼び出しを確認します。グループでは試行を展開して失敗したメンバーと最終回答者を確認します。グループ名だけでは実行モデルを特定できません。', 'Anfragezeile öffnen und tatsächliches Ziel, Modell, Fehlerphase und interne Aufrufe prüfen. Bei Gruppen die Versuche aufklappen, um fehlgeschlagene und schließlich antwortende Mitglieder zu sehen. Der Gruppenname allein nennt das ausführende Modell nicht.'],
    ['A real cloud request may incur provider charges. If no record appears, check that the app selected an 端云协同 model and started a new session, then refresh Requests. The remaining chapters cover routing groups, redaction and recovery.', '真实云端请求可能产生供应商费用。若没有记录，请确认应用已选择 端云协同 模型并新建会话，再刷新“请求”。后续章节介绍路由组、脱敏及恢复。', '真實雲端請求可能產生供應商費用。若沒有紀錄，請確認應用程式已選擇 端云协同 模型並建立新會話，再重新整理「請求」。後續章節介紹路由組、去識別化及復原。', '実際のクラウドリクエストは料金が発生する場合があります。記録がなければ端云协同のモデルと新規セッションを確認し、リクエストを更新します。続く章ではグループ、匿名化、復元を説明します。', 'Echte Cloud-Anfragen können Kosten verursachen. Fehlt ein Eintrag, 端云协同-Modellauswahl und neue Sitzung prüfen und Anfragen aktualisieren. Die folgenden Kapitel erklären Gruppen, Anonymisierung und Wiederherstellung.'],
    ['A routing group exposes several models as one group/<id> model. For example, put a primary cloud model first and a backup second, then select the group in your app.', '路由组把多个模型统一为一个 group/<id> 模型。例如把主用云端模型排第一、备用排第二，再在应用中选择这个组。', '路由組把多個模型統一為一個 group/<id> 模型。例如把主要雲端模型排第一、備用排第二，再在應用程式中選擇這個組。', '複数のモデルをgroup/<id>という一つのモデルとして公開します。例えば主クラウドモデルを先頭、予備を二番目に置き、アプリでこのグループを選びます。', 'Eine Gruppe stellt mehrere Modelle als ein group/<id>-Modell bereit. Zum Beispiel das primäre Cloud-Modell zuerst und ein Ersatzmodell danach eintragen und die Gruppe in der App wählen.'],
    ['Create a group and add members', '新建路由组并添加成员', '建立路由組並新增成員', 'グループを作りメンバーを追加', 'Gruppe erstellen und Mitglieder hinzufügen'],
    ['In Routing, click “＋ New routing group”, enter a name and use Add model to pick at least two available models. Drag members to set their order. Start with explicit members; patterns and nested groups are advanced options.', '在“路由”页点击“＋ 新建路由组”，填写名称，用“添加模型”选择至少两个可用模型。拖动成员调整顺序。先使用明确选定的成员，模式匹配及嵌套组可在熟悉后再配置。', '在「路由」頁點擊「＋ 新建路由組」，填寫名稱，用「新增模型」選擇至少兩個可用模型。拖動成員調整順序。先使用明確選定的成員，模式比對及巢狀組可在熟悉後再設定。', 'ルーティングで新規グループを押し、名前を入力してモデル追加から利用可能なモデルを二つ以上選びます。ドラッグで並べ替えます。まず個別に選択し、パターンや入れ子は慣れてから使います。', 'Unter Routing eine neue Gruppe anlegen, benennen und mindestens zwei verfügbare Modelle hinzufügen. Mitglieder durch Ziehen ordnen. Zunächst explizite Modelle verwenden; Muster und verschachtelte Gruppen sind erweiterte Optionen.'],
    ['Choose how requests are assigned', '选择请求分配策略', '選擇請求分配策略', 'リクエストの配分方法を選ぶ', 'Verteilung der Anfragen wählen'],
    ['For a primary and backup, choose In order. Take turns rotates healthy members; Weighted uses member weights. Manual uses the selected active member. Smart chooses according to request and member state. A strategy is not a guarantee that every failure can be retried.', '主备场景选择“按顺序”；“轮流”在健康成员间分配，“按权重”按成员权重分配，“手动”使用选中的生效成员，“智能”结合请求及成员状态选择。选择策略不代表所有失败都能重试兜底。', '主備情境選擇「按順序」；「輪流」在健康成員間分配，「按權重」依成員權重分配，「手動」使用選中的生效成員，「智慧」結合請求及成員狀態選擇。選擇策略不代表所有失敗都能重試備援。', '主系と予備なら順番を選びます。交代は正常なメンバーで回し、重み付けは比率で配分、手動は選んだメンバーを使います。スマートはリクエストと状態で選びます。すべての失敗で再試行できる保証はありません。', 'Für Haupt- und Ersatzmodell In Reihenfolge wählen. Abwechselnd verteilt auf gesunde Mitglieder, Gewichtet nach Gewichten, Manuell nutzt das aktive Mitglied und Smart berücksichtigt Anfrage und Zustand. Nicht jeder Fehler erlaubt einen erneuten Versuch.'],
    ['Save and expose the group to your app', '保存路由组并接入应用', '儲存路由組並接入應用程式', '保存してアプリで使う', 'Gruppe speichern und in der App nutzen'],
    ['Save and copy the group/<id> shown in the editor. In Agents, tick that routing group and move it to #1 if it should be the default. Connect, restart the app and select the group there. Creating a group alone does not change the app model.', '保存后可复制编辑器中的 group/<id>。回到 Agent 勾选这个路由组，需要作为默认时移到 #1。连接并重启应用，在应用里选择该组；仅创建路由组不会自动替换应用模型。', '儲存後可複製編輯器中的 group/<id>。回到 Agent 勾選這個路由組，需要作為預設時移到 #1。連線並重新啟動應用程式，在應用程式裡選擇該組；僅建立路由組不會自動替換應用程式模型。', '保存後にエディターのgroup/<id>をコピーできます。Agentでグループを選び、既定にするなら#1へ移動します。接続後アプリを再起動して選びます。グループを作るだけではアプリのモデルは変わりません。', 'Speichern und group/<id> aus dem Editor kopieren. Unter Agents die Gruppe wählen und bei Bedarf auf #1 setzen. Verbinden, App neu starten und dort die Gruppe wählen. Das Anlegen allein ändert das App-Modell nicht.'],
    ['Combine local and cloud models', '组合本地与云端模型', '組合本機與雲端模型', 'ローカルとクラウドを組み合わせる', 'Lokale und Cloud-Modelle kombinieren'],
    ['Add a ready local member and a cloud member to expose Edge-cloud. Short simple text can go local; unavailable or busy local runtime, images, tools, long context or complex tasks can go cloud. Check actual request details. Local routing alone does not redact cloud-bound content.', '加入已就绪的本地成员与云端成员后，可选择“端云协同”。短小简单文本可走本地；本地不可用或繁忙，以及图片、工具、长上下文或复杂任务可走云端。以请求详情为准，本地路由不会自动脱敏发往云端的内容。', '加入已就緒的本機成員與雲端成員後，可選擇「端雲協同」。短小簡單文本可走本機；本機不可用或忙碌，以及圖片、工具、長上下文或複雜任務可走雲端。以請求詳情為準，本機路由不會自動去識別化發往雲端的內容。', '準備済みローカルモデルとクラウドモデルを追加すると端末・クラウド協調を選べます。短い単純なテキストはローカルへ、使用不可・混雑時や画像、ツール、長文、複雑な作業はクラウドへ進む場合があります。詳細で確認してください。ローカルルーティングだけでは匿名化されません。', 'Ein bereites lokales und ein Cloud-Modell hinzufügen, um Edge-Cloud zu wählen. Kurze einfache Texte können lokal laufen; bei fehlender oder belegter Laufzeit, Bildern, Tools, langem Kontext oder komplexen Aufgaben kann die Cloud folgen. Details prüfen. Lokales Routing anonymisiert Cloud-Inhalte nicht automatisch.'],
    ['Automatic groups and advanced settings', '自动分组与进阶设置', '自動分組與進階設定', '自動グループと詳細設定', 'Automatische Gruppen und erweiterte Einstellungen'],
    ['Same-name models across providers may appear as automatic groups. Use Pin as custom to keep an editable group. Start with default timeouts and session settings; change weights, context or rules only when needed. Avoid cycles in nested groups and recheck app selections after disabling or deleting members.', '跨供应商的同名模型可能自动成组，点击“固定为自定义”后可保留并编辑。超时和会话设置先使用默认值，按需再改权重、上下文或规则。嵌套组不能形成循环，停用或删除成员后应重新检查应用选择。', '跨供應商的同名模型可能自動成組，點擊「固定為自訂」後可保留並編輯。逾時和會話設定先使用預設值，按需再改權重、上下文或規則。巢狀組不能形成循環，停用或刪除成員後應重新檢查應用程式選擇。', 'プロバイダー間の同名モデルは自動グループになる場合があります。カスタムとして固定すると保存・編集できます。タイムアウトやセッションは既定から始め、必要時に重み、コンテキスト、ルールを変えます。入れ子の循環を避け、メンバー削除後はアプリの選択を確認します。', 'Gleichnamige Modelle verschiedener Anbieter können automatisch gruppiert werden. Als benutzerdefiniert fixieren, um sie dauerhaft zu bearbeiten. Mit Standardwerten für Zeitlimits und Sitzungen beginnen. Gewichte, Kontext und Regeln bei Bedarf ändern. Zyklen vermeiden und nach Entfernen von Mitgliedern die App-Auswahl prüfen.'],
    ['For a group containing a local model, local redaction can prepare content before cloud dispatch and restore placeholders in responses. It is separate from choosing local execution and remains experimental.', '包含本地模型的路由组可在转云前执行本地脱敏，并在响应中还原占位符。脱敏与选择本地执行是不同能力，目前仍属实验性。', '包含本機模型的路由組可在轉雲前執行本機去識別化，並在回應中還原預留位置。去識別化與選擇本機執行是不同能力，目前仍屬實驗性。', 'ローカルモデルを含むグループではクラウド送信前に匿名化し、回答内のプレースホルダーを復元できます。ローカル実行の選択とは別の実験的な機能です。', 'In Gruppen mit lokalem Modell kann lokale Anonymisierung Inhalte vor dem Cloud-Versand vorbereiten und Platzhalter in Antworten wiederherstellen. Diese experimentelle Funktion ist von lokaler Ausführung getrennt.'],
    ['Enable redaction on the intended group', '在目标路由组开启脱敏', '在目標路由組開啟去識別化', '対象グループで匿名化を有効化', 'Anonymisierung für die Zielgruppe aktivieren'],
    ['Install and turn on the local model first. Edit the routing group, add its local member and choose Redact On, then save. In Agents, select this group; directly selecting a cloud model does not enable that group’s redaction pipeline.', '先安装并开启本地模型。编辑路由组，加入本地成员，将“本地模型脱敏”设为“开”后保存。在 Agent 中选择此组；直接选择云端模型不会启用这个组的脱敏管线。', '先安裝並開啟本機模型。編輯路由組，加入本機成員，選擇去識別化開啟後儲存。在 Agent 中選擇此組；直接選擇雲端模型不會啟用這個組的去識別化管線。', 'まずローカルモデルを導入してオンにします。グループを編集しローカルメンバーを追加、匿名化オンを選んで保存します。Agentでこのグループを選びます。クラウドモデルを直接選んでもグループの匿名化は有効になりません。', 'Zuerst lokales Modell installieren und einschalten. Gruppe bearbeiten, lokales Mitglied hinzufügen, Anonymisierung einschalten und speichern. Unter Agents diese Gruppe wählen. Ein direkt gewähltes Cloud-Modell aktiviert diese Gruppenpipeline nicht.'],
    ['Test with invented sensitive data', '用虚构敏感信息试用', '用虛構敏感資訊試用', '架空の機密情報で試す', 'Mit erfundenen sensiblen Daten testen'],
    ['In a new app session using the group, send an invented name or phone number. Open Usage → Redaction, refresh, filter by application ID or keyword and expand the record. Compare original input, redacted input and replacements; check request details for blocking errors.', '在使用该组的新会话里发送虚构姓名或手机号。打开“用量 → 脱敏”，刷新后按应用标识或关键词筛选，展开记录，对照原始输入、脱敏后输入与替换映射；如被阻断，再到请求详情查看错误。', '在使用該組的新會話裡傳送虛構姓名或手機號碼。開啟「用量 → 去識別化」，重新整理後依應用程式識別碼或關鍵字篩選，展開紀錄，對照原始輸入、去識別化後輸入與替換對應；如被阻擋，再到請求詳情查看錯誤。', 'このグループの新規セッションで架空の氏名や電話番号を送ります。使用量の匿名化タブを更新し、アプリIDやキーワードで絞って記録を展開します。原文、匿名化後、置換を比較し、遮断時はリクエスト詳細のエラーを見ます。', 'In einer neuen Sitzung mit der Gruppe erfundenen Namen oder Telefonnummer senden. Nutzung → Anonymisierung öffnen, aktualisieren, nach App-ID oder Stichwort filtern und Eintrag aufklappen. Original, anonymisierten Text und Ersetzungen vergleichen; bei Blockaden Anfragedetails prüfen.'],
    ['Read the scope and clean up records', '了解展示范围并清理记录', '瞭解展示範圍並清理紀錄', '表示範囲を理解して記録を消去', 'Umfang verstehen und Einträge löschen'],
    ['The comparison shows the latest user input, not the full outbound payload or all conversation history. Local observation records contain original text and replacement values. They are retained for up to seven days and 500 request IDs by default; Clear records asks for confirmation before deletion.', '对比只展示最新一轮用户输入，不是完整出站报文或全部会话历史。本地观测记录会保存原文和替换值，默认最多保留七天、500 个请求标识；点击“清空记录”后还需确认才会删除。', '對比只顯示最新一輪使用者輸入，不是完整對外報文或全部會話歷史。本機觀測紀錄會儲存原文和替換值，預設最多保留七天、500 個請求識別碼；點擊「清空紀錄」後還需確認才會刪除。', '比較は最新ユーザー入力のみで、送信全体や全履歴ではありません。ローカル記録には原文と置換値が保存され、既定では最大7日・500リクエストIDを保持します。記録消去は確認後に実行されます。', 'Der Vergleich zeigt die letzte Benutzereingabe, nicht die gesamte ausgehende Nachricht oder Historie. Lokale Einträge speichern Originaltext und Ersetzungen, standardmäßig bis zu sieben Tage und 500 Anfrage-IDs. Einträge löschen erfordert eine Bestätigung.'],
    ['If redaction preparation fails or exceeds its shared 30-second budget, the cloud request is blocked. Inspect the error and local runtime; recognition can miss or misidentify data. Turning Redact Off sends future eligible requests without this group’s redaction.', '脱敏准备失败或超过共享的 30 秒预算时会阻断转云。请查看错误和本地运行状态；识别可能漏报或误报。将“本地模型脱敏”设为“关”后，后续适用请求将不再经过此组的脱敏处理。', '去識別化準備失敗或超過共享的 30 秒預算時會阻擋轉雲。請查看錯誤和本機執行狀態；識別可能漏報或誤報。關閉後，後續適用請求將不再經過此組的去識別化處理。', '匿名化の準備が失敗、または共通の30秒予算を超えるとクラウド送信は遮断されます。エラーとローカル状態を確認します。検出漏れや誤検出はあり得ます。匿名化オフでは以降の対象リクエストにこの処理が適用されません。', 'Scheitert die Vorbereitung oder überschreitet sie das gemeinsame Budget von 30 Sekunden, wird die Cloud-Anfrage blockiert. Fehler und Laufzeit prüfen; Erkennungsfehler sind möglich. Bei ausgeschalteter Anonymisierung laufen spätere betroffene Anfragen ohne diese Gruppenanonymisierung.'],
    ['Use the symptom below to decide what to inspect. After changing settings, retry a simple request in a new app session and confirm its record.', '按下面的现象检查。修改配置后，请在应用新会话中重试一条简单请求，并核对记录。', '依下面的現象檢查。修改設定後，請在應用程式新會話中重試一筆簡單請求，並核對紀錄。', '症状に合った項目を確認します。設定変更後は新規セッションで単純なリクエストを試し、記録を確認します。', 'Passendes Symptom prüfen. Nach Änderungen eine einfache Anfrage in einer neuen App-Sitzung senden und den Eintrag kontrollieren.'],
    ['No models or no connection', '没有模型或无法连接', '沒有模型或無法連線', 'モデルがない・接続できない', 'Keine Modelle oder keine Verbindung'],
    ['Save a provider first and check its model list, then select at least one available model in Agents. If the host directory is missing, launch the app once to create it, close it and retry the connection. Use the setup command’s explicit directory option if detection remains ambiguous.', '先保存供应商并查看模型列表，再在 Agent 中至少选择一个可用模型。未检测到宿主目录时，先启动一次应用生成配置，关闭后重试。仍无法确定目录时，使用接入配置命令显式指定目录。', '先儲存供應商並查看模型清單，再在 Agent 中至少選擇一個可用模型。未偵測到宿主目錄時，先啟動一次應用程式產生設定，關閉後重試。仍無法確定目錄時，使用接入設定命令明確指定目錄。', 'プロバイダーと一覧を確認し、Agentで利用可能なモデルを一つ以上選びます。ホストディレクトリがなければ一度アプリを起動して設定を作り、閉じて再試行します。検出が曖昧ならセットアップコマンドでディレクトリを指定します。', 'Anbieter speichern, Modellliste prüfen und unter Agents mindestens ein verfügbares Modell wählen. Fehlt das Host-Verzeichnis, App einmal starten, schließen und erneut verbinden. Bei uneindeutiger Erkennung das Verzeichnis im Setup-Befehl explizit angeben.'],
    ['Connected, but the app uses another model', '已连接，但应用仍用其他模型', '已連線，但應用程式仍用其他模型', '接続済みでも別モデルが使われる', 'Verbunden, aber App nutzt anderes Modell'],
    ['Fully quit and reopen the app, choose its 端云协同 provider and the selected model or group, then start a new session. In Loomy, also inspect project-level .opencode settings. If 端云协同 says Reconnect required, select available models and reconnect.', '完全退出并重开应用，选择 端云协同 供应商及所选模型或组，再新建会话。Loomy 还需检查项目级 .opencode 设置。端云协同 显示“需要重新连接”时，请重新选择可用模型并连接。', '完全退出並重開應用程式，選擇 端云协同 供應商及所選模型或組，再建立新會話。Loomy 還需檢查專案層級 .opencode 設定。端云协同 顯示「需要重新連線」時，請重新選擇可用模型並連線。', 'アプリを完全終了して再度開き、端云协同のプロバイダーとモデルまたはグループを選び新規セッションを作ります。Loomyではプロジェクトの.opencodeも確認します。再接続が必要と出たら利用可能なモデルを選び直して接続します。', 'App vollständig beenden und öffnen, 端云协同 mit gewünschtem Modell oder Gruppe wählen und neue Sitzung starten. Bei Loomy auch .opencode im Projekt prüfen. Bei Erneut verbinden verfügbare Modelle wählen und erneut verbinden.'],
    ['A request fails or the local model is slow', '请求失败或本地模型很慢', '請求失敗或本機模型很慢', 'リクエスト失敗・ローカルが遅い', 'Anfrage schlägt fehl oder lokales Modell ist langsam'],
    ['Open Usage → Requests and inspect the error code, failure stage and attempts. For cloud authentication or model errors, check the provider key, platform, protocol and model permission. For local errors, check installation, health, memory and effective CPU/GPU backend. For redaction blocks, inspect Usage → Redaction.', '打开“用量 → 请求”，查看错误码、失败阶段和尝试记录。云端认证或模型错误需核对供应商密钥、平台、协议及模型权限；本地错误需检查安装、健康、内存和实际 CPU/GPU 后端；脱敏阻断则查看“用量 → 脱敏”。', '開啟「用量 → 請求」，查看錯誤碼、失敗階段和嘗試紀錄。雲端認證或模型錯誤需核對供應商金鑰、平台、協定及模型權限；本機錯誤需檢查安裝、健康、記憶體和實際 CPU/GPU 後端；去識別化阻擋則查看「用量 → 去識別化」。', '使用量のリクエストでエラーコード、失敗段階、試行を確認します。クラウドならキー、プラットフォーム、プロトコル、モデル権限を確認。ローカルなら導入、健全性、メモリ、実効CPU/GPUを確認。匿名化で遮断されたら匿名化タブを確認します。', 'Unter Nutzung → Anfragen Fehlercode, Fehlerphase und Versuche prüfen. Bei Cloud-Fehlern Schlüssel, Plattform, Protokoll und Modellrechte prüfen. Lokal Installation, Zustand, Speicher und tatsächliches CPU/GPU-Backend kontrollieren. Bei Anonymisierungsblockaden den entsprechenden Nutzungstab öffnen.'],
    ['Disconnect safely or reopen management', '安全断开或重新打开管理页', '安全中斷或重新開啟管理頁', '安全に切断・管理画面を開き直す', 'Sicher trennen oder Verwaltung erneut öffnen'],
    ['Finish tasks and close the app, then use Details → Disconnect & restore or turn off its Agent connection switch. If restoration reports a conflict, preserve the modified files and recovery record before retrying. On Windows, use start.cmd to open the actual management address; the service may choose another loopback port when needed.', '结束任务并关闭应用，再点击“详情 → 断开并还原”或关闭 Agent 连接开关。还原提示冲突时，请先保留改动文件及恢复记录再处理。Windows 可通过 start.cmd 打开实际管理地址；原端口被占用时服务可能改用其他回环端口。', '結束任務並關閉應用程式，再點擊「詳情 → 中斷並還原」或關閉 Agent 連線開關。還原提示衝突時，請先保留變更檔案及復原紀錄再處理。Windows 可透過 start.cmd 開啟實際管理位址；原連接埠被占用時服務可能改用其他回環連接埠。', '作業を終えてアプリを閉じ、詳細の切断と復元かAgentスイッチを使います。競合が出たら変更済みファイルと復元記録を保持して対処します。Windowsではstart.cmdから実際の管理URLを開けます。ポート使用中なら別のループバックポートになる場合があります。', 'Aufgaben beenden, App schließen und unter Details Trennen & wiederherstellen wählen oder Agent-Schalter ausschalten. Bei Konflikten geänderte Dateien und Wiederherstellungsprotokoll erhalten. Unter Windows öffnet start.cmd die tatsächliche Verwaltungsadresse; ein belegter Port kann zum Wechsel auf einen anderen Loopback-Port führen.'],
  ]) for (const [language, value] of [['zh', zh], ['zh-TW', tw], ['ja', ja], ['de', de]]) (I18N[language] ||= {})[key] = value;
  translateStatic();

  const key = 'wefto.tutorial.v1';
  const fields = ['version', 'step', 'route', 'app', 'dismissed', 'completed'];
  const defaults = () => ({ version: 1, step: 0, route: 'local', app: 'workbuddy', dismissed: false, completed: false });
  let state = defaults();
  try {
    const saved = JSON.parse(localStorage.getItem(key) ?? localStorage.getItem('api-router.tutorial.v1'));
    if (saved && !Array.isArray(saved) && typeof saved === 'object' &&
        Object.keys(saved).length === fields.length && fields.every((field) => Object.hasOwn(saved, field)) &&
        saved.version === 1 && Number.isInteger(saved.step) && saved.step >= 0 && saved.step <= 8 &&
        ['local', 'cloud'].includes(saved.route) && ['workbuddy', 'loomy'].includes(saved.app) &&
        typeof saved.dismissed === 'boolean' && typeof saved.completed === 'boolean') {
      state = { version: 1, step: saved.step, route: saved.route, app: saved.app, dismissed: saved.dismissed, completed: saved.completed };
      if (state.route === 'cloud' && state.step === 4) state.step = 3;
    }
  } catch {}
  const save = () => { try { localStorage.setItem(key, JSON.stringify(state)); } catch {} };
  const modal = $('#tutorialModal');
  if (!modal) return;
  const dialog = modal.querySelector('.tutorial-dialog');
  const content = $('#tutorialContent');
  const reading = modal.querySelector('.tutorial-reading');
  // Keep the first six chapter IDs stable for existing reading progress.
  const names = ['Meet 端云协同', 'Connect a provider', 'Set up your app', 'Choose your local route', 'Install and try', 'Your first request', 'Use routing groups', 'Redact and inspect', 'Troubleshoot and restore'];
  let opened = false;
  let returnFocus = null;
  let isolated = [];
  let initialized = false, savedOnService = false, saving = null;
  function rememberDisplay() {
    if (savedOnService || saving || !managementToken) return;
    const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 3000);
    saving = managementFetch('/api/tutorial/dismiss', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', signal: controller.signal,
    }).then(response => {
      if (response.ok) {
        savedOnService = true;
        if (serviceInfo) serviceInfo.tutorialDismissed = true;
      }
    }).catch(() => {}).finally(() => { clearTimeout(timeout); saving = null; });
  }
  function connected(info) {
    savedOnService = info.tutorialDismissed === true;
    // Carry forward existing browser reading preferences on upgrade.
    if (state.dismissed || state.completed) {
      if (info.tutorialDismissed === false) rememberDisplay();
      return;
    }
    if (initialized || typeof info.tutorialDismissed !== 'boolean') return;
    initialized = true;
    if (!info.tutorialDismissed) open();
  }
  const visibleSteps = () => names.map((_, step) => step).filter(step => state.route !== 'cloud' || step !== 4);
  const paragraph = (copy, className = 'tutorial-lead') => el('p', className, t(copy));
  const note = (copy, warning = false) => el('div', 'tutorial-note' + (warning ? ' tutorial-warning' : ''), t(copy));
  const focus = (node) => node?.focus({ preventScroll: true });
  function button(copy, className, marker, action) {
    const node = el('button', className, t(copy));
    node.type = 'button';
    if (marker) node.dataset.tutorialFocus = marker;
    node.onclick = action;
    return node;
  }
  function sequence(rows) {
    const list = el('ol', 'tutorial-sequence');
    for (const [index, [title, copy]] of rows.entries()) {
      const item = el('li');
      const number = el('span', 'tutorial-number', String(index + 1));
      number.setAttribute('aria-hidden', 'true');
      const text = el('div');
      text.append(el('strong', '', t(title)), paragraph(copy, 'tutorial-inlinehint'));
      item.append(number, text);
      list.append(item);
    }
    return list;
  }
  function details(title, copy) {
    const node = el('details', 'tutorial-details');
    const summary = el('summary', '', t(title));
    summary.dataset.tutorialFocus = 'details-' + state.step;
    node.append(summary, paragraph(copy, 'tutorial-inlinehint'));
    return node;
  }
  function choices(label, entries, selected, choose) {
    const group = el('div', 'tutorial-choices');
    group.setAttribute('role', 'group');
    group.setAttribute('aria-label', t(label));
    for (const [id, copy] of entries) {
      const choice = button(copy, 'tutorial-choice', id, () => { choose(id); save(); render(); });
      choice.dataset[label === 'Choose app instructions' ? 'app' : 'route'] = id;
      choice.setAttribute('aria-pressed', String(selected === id));
      group.append(choice);
    }
    return group;
  }
  function lesson() {
    const fragment = document.createDocumentFragment();
    if (state.step === 0) {
      fragment.append(paragraph('Connect a cloud provider, bind it to your app, then add a local model if you need one. You can close this guide and continue at any time.'));
      const flow = el('div', 'tutorial-flow');
      flow.setAttribute('role', 'img');
      flow.setAttribute('aria-label', t('Request path: your app, 端云协同, cloud or local model'));
      for (const [index, name] of ['Your app', '端云协同', 'Cloud or local model'].entries()) {
        if (index) { const arrow = el('span', 'tutorial-arrow', '→'); arrow.setAttribute('aria-hidden', 'true'); flow.append(arrow); }
        flow.append(el('div', 'tutorial-node' + (index === 1 ? ' tutorial-node-router' : ''), t(name)));
      }
      fragment.append(flow);
      const features = el('div', 'tutorial-features');
      for (const [title, copy] of [
        ['Connect cloud first', 'Have your provider API key ready, then choose the cloud model for WorkBuddy or Loomy.'],
        ['Local models are optional', 'Ordinary cloud use does not require a model download. Local Q4_K_M and Q8_0 routing and execution are experimental.'],
      ]) {
        const feature = el('div', 'tutorial-feature');
        feature.append(el('strong', '', t(title)), paragraph(copy, 'tutorial-inlinehint'));
        features.append(feature);
      }
      fragment.append(features, note('The page connects to local management automatically. If disconnected, check the service and use Refresh now. A management token is different from a provider API key.'), note('Reading does not download models, enable trials or send model requests. Configuration actions remain yours.'));
    } else if (state.step === 1) {
      fragment.append(paragraph('A provider supplies the cloud service. In Providers, add the platform you use and save its connection details. Choose the app model in the next chapter.'));
      const fields = el('dl', 'tutorial-fields');
      for (const [title, copy] of [
        ['Provider and platform', 'Choose your existing provider and confirm the platform or region that issued your key.'],
        ['API protocol', 'Use the protocol supported by that provider. The model name alone does not determine the protocol.'],
        ['API base address', 'Copy the base address from the provider console, without /chat/completions or /messages.'],
        ['Provider API key', 'Use an API key issued by this provider. Never paste the local management token here.'],
      ]) {
        const row = el('div', 'tutorial-field');
        row.append(el('dt', 'tutorial-field-name', t(title)), el('dd', 'tutorial-field-value', t(copy)));
        fields.append(row);
      }
      fragment.append(fields, note('Save the provider, then check its model list. A failed model-list fetch does not undo the save. A preset or cached list is only a candidate list; confirm access with a real request from your app.'));
    } else if (state.step === 2) {
      fragment.append(paragraph('In Agents, find your app, select its models and turn on its connection switch. Start with one cloud model; add more models or groups after the first request works.'), choices('Choose app instructions', [['workbuddy', 'WorkBuddy'], ['loomy', 'Loomy']], state.app, (id) => { state.app = id; }));
      fragment.append(sequence([
        ['Pick models and set the default', 'Open “Choose models”, search by provider or model and tick the models you need. Use ↑ / ↓ to move the desired default to #1. Changes save automatically. For an unlisted model, add provider/model using the saved provider ID and exact model ID.'],
        ['Turn on the connection switch', 'Finish running tasks and close the app before changing its connection. Turn on the switch in its Agent row and wait for Connected. 端云协同 writes the selected model list to the detected host configuration; this does not verify cloud generation access.'],
        ['Restart the app and start a new session', state.app === 'loomy'
          ? 'Restart Loomy, choose 端云协同 and start a new session. Existing sessions and project-level .opencode settings may keep a different model or provider.'
          : 'Finish existing tasks, restart WorkBuddy and choose the 端云协同 model. Send your first request in a new session.'],
      ]), note('Only selected models appear in the app. Open Details to review the model list and new-session default. Changes while connected also update the host configuration; fully quit and reopen the app to reload it.'));
      if (state.app === 'loomy') fragment.append(note('Loomy uses the provider credentials stored in 端云协同. If connection is rejected because of a host extension, inspect the reported extension conflict before retrying.'));
    } else if (state.step === 3) {
      fragment.append(paragraph('A local model can judge and answer suitable requests on this computer. Successful local completion may reduce cloud calls; speed, quality and actual savings depend on the device and task.'));
      const model = el('div', 'tutorial-model');
      const label = el('div', 'tutorial-model-label');
      label.append(el('strong', '', t('Spark-X2.6-4B · Q4_K_M / Q8_0')), paragraph('Both tiers use the same installation and trial flow', 'tutorial-inlinehint'));
      model.append(label, el('span', 'tutorial-tag', t('Experimental')));
      fragment.append(model, paragraph('Choose Q4_K_M or Q8_0 based on available memory, disk and supported runtime, then confirm with device detection. You can keep using cloud without installing a model.', 'tutorial-inlinehint'), choices('Choose a learning route', [['local', 'Learn local installation and trial'], ['cloud', 'Use cloud first']], state.route, (id) => { state.route = id; }), note("Local trial and redaction have separate setup and readiness checks. Enabling local routing does not automatically redact cloud requests; cloud traffic still reaches your selected provider.", true), details('Understand local trial limits', 'Engineering checks are not quality acceptance. Local answers, cloud-routing decisions and answers after tool calls may fail or time out. Use actual request records to judge results.'));
    } else if (state.step === 4) {
      fragment.append(paragraph('Use Local model to perform these actions in order. Installation prepares files; the installed-model switch starts or stops local use.'), sequence([
        ['Detect the device', 'Check memory, disk and runtime conditions. A recommendation is not a download or a quality guarantee.'],
        ['Select Q4_K_M or Q8_0', 'Choose a fixed Q4_K_M or Q8_0 model. Both use the same download, configuration and capability-check flow.'],
        ['Download and install the model', 'Click “Download and install” to authorize installation of the selected model and dependencies. Follow download progress and integrity checks; use Cancel download to pause, then retry installation after an interruption.'],
        ['Turn on the installed model', 'In “Configured on-device models”, turn on the model switch. 端云协同 selects and loads it, runs local capability checks and attempts to enable the experimental trial when checks pass. Read any errors and current runtime state before using it.'],
        ['Check the backend and connect the model', 'Inspect Inference backend and Check process health. A detected GPU does not prove acceleration: check the effective backend and loaded layers. CPU fallback can still run. In Agents, select local/<model-id> or a routing group containing it, then restart the app.'],
      ]), note('Enabled models stay loaded while 端云协同 runs and the on/off choice is remembered on restart. Closing the page does not stop the service. To release local resources, turn off the installed model switch.'), details('Understand local trial limits', 'Engineering checks are not quality acceptance. Local answers, cloud-routing decisions and answers after tool calls may fail or time out. Use actual request records to judge results.'), button('Skip installation for now', 'text tutorial-skip', 'skip-install', () => { state.route = 'cloud'; setStep(5); }));
    } else if (state.step === 5) {
      fragment.append(paragraph('After configuration, restart the connected app, choose 端云协同 and open a new session. Send a simple request there, then return here to inspect actual results.'), el('blockquote', 'tutorial-quote', t('Try: briefly explain what you can help me with.')));
      const rows = el('div', 'tutorial-resultrows');
      for (const [title, copy] of [
        ['Usage → Requests', 'Open the request detail for status, actual tokens, latency and internal calls. An error is a result to investigate, not a successful local answer.'],
        ['Request details and fallback attempts', 'Click a request row to inspect the actual destination, model, failure stage and internal calls. For a routing group, expand its attempts to see which member failed and which eventually answered. The group name alone does not identify the executing model.'],
        ['Usage → Overview', 'Review aggregates within the retained window. Only requests through 端云协同 are counted. Unreported tokens remain unknown; reference prices and savings are estimates, not bills.'],
      ]) {
        const row = el('div', 'tutorial-resultrow');
        row.append(el('strong', '', t(title)), paragraph(copy, 'tutorial-inlinehint'));
        rows.append(row);
      }
      fragment.append(rows, note('A real cloud request may incur provider charges. If no record appears, check that the app selected an 端云协同 model and started a new session, then refresh Requests. The remaining chapters cover routing groups, redaction and recovery.'));
    } else if (state.step === 6) {
      fragment.append(paragraph('A routing group exposes several models as one group/<id> model. For example, put a primary cloud model first and a backup second, then select the group in your app.'), sequence([
        ['Create a group and add members', 'In Routing, click “＋ New routing group”, enter a name and use Add model to pick at least two available models. Drag members to set their order. Start with explicit members; patterns and nested groups are advanced options.'],
        ['Choose how requests are assigned', 'For a primary and backup, choose In order. Take turns rotates healthy members; Weighted uses member weights. Manual uses the selected active member. Smart chooses according to request and member state. A strategy is not a guarantee that every failure can be retried.'],
        ['Save and expose the group to your app', 'Save and copy the group/<id> shown in the editor. In Agents, tick that routing group and move it to #1 if it should be the default. Connect, restart the app and select the group there. Creating a group alone does not change the app model.'],
      ]), details('Combine local and cloud models', 'Add a ready local member and a cloud member to expose Edge-cloud. Short simple text can go local; unavailable or busy local runtime, images, tools, long context or complex tasks can go cloud. Check actual request details. Local routing alone does not redact cloud-bound content.'), details('Automatic groups and advanced settings', 'Same-name models across providers may appear as automatic groups. Use Pin as custom to keep an editable group. Start with default timeouts and session settings; change weights, context or rules only when needed. Avoid cycles in nested groups and recheck app selections after disabling or deleting members.'));
    } else if (state.step === 7) {
      fragment.append(paragraph('User input redaction'), note('System prompts and new information from tools are not scanned.'));
      fragment.append(paragraph('For a group containing a local model, local redaction can prepare content before cloud dispatch and restore placeholders in responses. It is separate from choosing local execution and remains experimental.'), sequence([
        ['Check redaction startup permission', 'Redaction startup permission is enabled by default. In Settings → Service management, check Experimental redaction at startup. If disabled, stop 端云协同, then start the same installed EXE with service start --experimental-redaction. The group’s first local member must match the enabled model. With group redaction on, local preparation precedes the cloud answer.'],
        ['Enable redaction on the intended group', 'Install and turn on the local model first. Edit the routing group, add its local member and choose Redact On, then save. In Agents, select this group; directly selecting a cloud model does not enable that group’s redaction pipeline.'],
        ['Test with invented sensitive data', 'In a new app session using the group, send an invented name or phone number. Open Usage → Redaction, refresh, filter by application ID or keyword and expand the record. Compare original input, redacted input and replacements; check request details for blocking errors.'],
        ['Read the scope and clean up records', 'The comparison shows the latest user input, not the full outbound payload or all conversation history. Local observation records contain original text and replacement values. They are retained for up to seven days and 500 request IDs by default; Clear records asks for confirmation before deletion.'],
      ]), note('If redaction preparation fails or exceeds its shared 30-second budget, the cloud request is blocked. Inspect the error and local runtime; recognition can miss or misidentify data. Turning Redact Off sends future eligible requests without this group’s redaction.', true));
    } else {
      fragment.append(paragraph('Use the symptom below to decide what to inspect. After changing settings, retry a simple request in a new app session and confirm its record.'), sequence([
        ['No models or no connection', 'Save a provider first and check its model list, then select at least one available model in Agents. If the host directory is missing, launch the app once to create it, close it and retry the connection. Use the setup command’s explicit directory option if detection remains ambiguous.'],
        ['Connected, but the app uses another model', 'Fully quit and reopen the app, choose its 端云协同 provider and the selected model or group, then start a new session. In Loomy, also inspect project-level .opencode settings. If 端云协同 says Reconnect required, select available models and reconnect.'],
        ['A request fails or the local model is slow', 'Open Usage → Requests and inspect the error code, failure stage and attempts. For cloud authentication or model errors, check the provider key, platform, protocol and model permission. For local errors, check installation, health, memory and effective CPU/GPU backend. For redaction blocks, inspect Usage → Redaction.'],
        ['Disconnect safely or reopen management', 'Finish tasks and close the app, then use Details → Disconnect & restore or turn off its Agent connection switch. If restoration reports a conflict, preserve the modified files and recovery record before retrying. On Windows, use start.cmd to open the actual management address; the service may choose another loopback port when needed.'],
      ]), details('Manage the service and startup', 'Open Settings → Service management to inspect the running program and login startup setting. Before stopping or restarting 端云协同, acknowledge that the page will disconnect. Stopping keeps host connections configured; use your usual launcher to start again. After a restart, the page reconnects automatically.'), note('Finishing marks this guide as read only. Provider setup, host connection and local trial readiness must be checked in their own pages and real requests.'));
    }
    return fragment;
  }
  const titles = ['Start with your first AI connection', 'Add your AI provider', 'Select models, then connect your app', 'Do I need a local model?', 'Install and turn on a local model', 'Send a request and see where it went', 'One group, several model choices', 'Enable redaction and check the result', 'Find the cause and restore your app'];
  const destinations = ['agents', 'providers', 'agents', 'local-model', 'local-model', 'usage', 'routing', 'usage', 'agents'];
  const goLabels = ['Go to Agents', 'Go to Providers', 'Go to Agents', 'Go to Local model', 'Go to Local model', 'Go to Requests', 'Go to Routing', 'Go to Redaction records', 'Go to Agents'];
  function render() {
    const active = document.activeElement;
    const marker = modal.contains(active) ? active.dataset.tutorialFocus : null;
    const scroll = reading.scrollTop;
    $('#tutorialTitle').textContent = t(titles[state.step]);
    $('#tutorialChapter').textContent = t(names[state.step]);
    $('#tutorialSteps').setAttribute('aria-label', t('Tutorial chapters'));
    $('#tutorialSteps').replaceChildren(...visibleSteps().map((step) => {
      const node = button(names[step], 'tutorial-step', 'step-' + step, () => setStep(step));
      node.dataset.step = String(step);
      node.replaceChildren(el('span', 'tutorial-count', String(step + 1)), el('span', 'tutorial-step-label', t(names[step])));
      node.setAttribute('aria-label', t('Chapter {number}: {name}', { number: step + 1, name: t(names[step]) }));
      if (step === state.step) node.setAttribute('aria-current', 'step');
      return node;
    }));
    content.replaceChildren(lesson());
    reading.scrollTop = scroll;
    const steps = visibleSteps();
    $('#tutorialProgress').textContent = t('Reading progress {current} / {total}', { current: steps.indexOf(state.step) + 1, total: steps.length });
    $('#tutorialClose').setAttribute('aria-label', t('Close guide'));
    $('#tutorialClose').setAttribute('title', t('Close guide'));
    $('#tutorialDismiss').textContent = t('Skip for now');
    $('#tutorialBack').textContent = t('Previous step');
    $('#tutorialBack').disabled = state.step === 0;
    $('#tutorialNext').textContent = t(state.step === 0 ? 'Start learning' : state.step === names.length - 1 ? 'Finish reading' : state.step === 3 && state.route === 'cloud' ? 'Skip local installation and continue' : 'Next step');
    $('#tutorialGo').textContent = t(goLabels[state.step]);
    for (const id of ['tutorialOpen', 'tutorialSettings']) {
      const node = $('#' + id);
      if (node) { node.textContent = t('Usage guide'); node.setAttribute('aria-label', t('Usage guide')); }
    }
    $('#tutorialResume').hidden = state.completed;
    $('#tutorialContinue').textContent = t(state.completed ? 'Read again' : 'Continue reading');
    $('#tutorialSummary').textContent = state.completed ? t('Guide read. Check actual settings and request results to confirm readiness.') : t('Continue from “{chapter}”. Reading progress is separate from configuration.', { chapter: t(names[state.step]) });
    if (opened && marker) {
      const replacement = [...modal.querySelectorAll('[data-tutorial-focus]')].find((node) => node.dataset.tutorialFocus === marker);
      focus(replacement || $('#tutorialNext'));
    } else if (opened && modal.contains(active) && (!active.isConnected || active.disabled)) focus($('#tutorialNext'));
  }
  function setStep(step) {
    state.step = step;
    save();
    render();
    reading.scrollTop = 0;
  }
  function isolate() {
    isolated = [...document.querySelectorAll('body > header, body > aside, body > main, body > footer, .modal')]
      .filter((node) => node !== modal && !node.contains(modal) && !modal.contains(node))
      .map((node) => ({ node, inert: node.inert, attribute: node.hasAttribute('inert') }));
    for (const { node } of isolated) node.inert = true;
  }
  function restore() {
    for (const { node, inert, attribute } of isolated) {
      node.inert = inert;
      if (attribute) node.setAttribute('inert', '');
      else if (!inert) node.removeAttribute('inert');
    }
    isolated = [];
  }
  function open(event) {
    if (opened) return;
    const invoker = event?.currentTarget;
    returnFocus = invoker instanceof HTMLElement && invoker.isConnected ? invoker : document.activeElement;
    opened = true;
    // Remember the first display immediately so a refresh never forces it open again.
    state.dismissed = true;
    save();
    rememberDisplay();
    modal.hidden = false;
    isolate();
    render();
    focus($('#tutorialNext'));
  }
  function close() {
    if (!opened) return;
    state.dismissed = true;
    save();
    rememberDisplay();
    opened = false;
    modal.hidden = true;
    restore();
    render();
    if (returnFocus?.isConnected && !returnFocus.closest('[inert]') && returnFocus.getClientRects().length) focus(returnFocus);
    else focus($('#tutorialOpen'));
    returnFocus = null;
  }
  for (const id of ['tutorialOpen', 'tutorialSettings', 'tutorialContinue']) $('#' + id)?.addEventListener('click', open);
  $('#tutorialClose').addEventListener('click', close);
  $('#tutorialDismiss').addEventListener('click', close);
  $('#tutorialBack').addEventListener('click', () => { const steps = visibleSteps(); setStep(steps[Math.max(0, steps.indexOf(state.step) - 1)]); });
  $('#tutorialNext').addEventListener('click', () => {
    if (state.step === names.length - 1) { state.completed = true; close(); return; }
    const steps = visibleSteps();
    setStep(steps[steps.indexOf(state.step) + 1]);
  });
  $('#tutorialGo').addEventListener('click', () => {
    const step = state.step;
    close();
    // A guide link is navigation only. Opening the local-model page from the
    // guide must not start its first-run hardware probe as a side effect.
    if (step === 3 || step === 4) window.routerLocalSkipDetectionOnce = true;
    show(destinations[step]);
    if (step === 5) $('#usageTab-requests')?.click();
    if (step === 7) $('#usageTab-redaction')?.click();
  });
  document.addEventListener('keydown', (event) => {
    if (!opened) return;
    if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); close(); return; }
    if (event.key !== 'Tab') return;
    const targets = [...dialog.querySelectorAll('button:not(:disabled), a[href], input:not(:disabled), textarea:not(:disabled), summary, [tabindex]:not([tabindex="-1"])')]
      .filter((node) => !node.closest('[hidden], [inert]') && node.getClientRects().length && getComputedStyle(node).visibility !== 'hidden');
    if (!targets.length) { event.preventDefault(); focus(dialog); return; }
    // Safari's keyboard settings can skip buttons in its default Tab order.
    // Choose every modal target explicitly; keyboard focus may scroll the
    // reading pane normally, while entry and exit keep preventScroll.
    const current = targets.indexOf(document.activeElement);
    const next = current < 0 ? (event.shiftKey ? targets.length - 1 : 0)
      : (current + (event.shiftKey ? -1 : 1) + targets.length) % targets.length;
    event.preventDefault();
    targets[next].focus();
  }, true);
  window.routerTutorialOpen = open;
  window.routerTutorialRender = render;
  window.routerTutorialConnected = connected;
  render();
  if (serviceInfo) connected(serviceInfo);
})();
