'use strict';
(() => {
  for (const [key, zh, tw, ja, de] of [
    ['Service management', '服务管理', '服務管理', 'サービス管理', 'Dienstverwaltung'],
    ['Process ID', '进程 ID', '程序 ID', 'プロセス ID', 'Prozess-ID'],
    ['Program location', '程序位置', '程式位置', 'プログラムの場所', 'Programmpfad'],
    ['Start at login', '登录时启动', '登入時啟動', 'ログイン時に起動', 'Bei Anmeldung starten'],
    ['Enable autostart', '开启自启', '開啟自動啟動', '自動起動を有効化', 'Autostart aktivieren'],
    ['Disable autostart', '关闭自启', '關閉自動啟動', '自動起動を無効化', 'Autostart deaktivieren'],
    ['Autostart is unavailable on this platform.', '此平台不支持登录自启。', '此平台不支援登入自動啟動。', 'この環境ではログイン時の起動を利用できません。', 'Autostart ist auf dieser Plattform nicht verfügbar.'],
    ['Autostart status is unknown. Refresh before changing it.', '自启状态未知，请刷新后再更改。', '自動啟動狀態未知，請重新整理後再變更。', '自動起動の状態が不明です。更新してから変更してください。', 'Autostart-Status unbekannt. Vor Änderungen aktualisieren.'],
    ['Experimental redaction at startup', '启动时实验脱敏', '啟動時實驗去識別化', '起動時の実験的匿名化', 'Experimentelle Anonymisierung beim Start'],
    ['Stop 端云协同', '停止 端云协同', '停止 端云协同', '端云协同 を停止', '端云协同 stoppen'],
    ['Restart 端云协同', '重启 端云协同', '重新啟動 端云协同', '端云协同 を再起動', '端云协同 neu starten'],
    ['I understand this will disconnect the page.', '我了解此操作会断开页面连接。', '我了解此操作會中斷頁面連線。', 'この操作でページが切断されることを理解しました。', 'Ich verstehe, dass dieser Vorgang die Seite trennt.'],
    ['Stopping disconnects this page. Active requests will be drained or canceled. Host connections stay configured.', '停止后此页面将失联，正在处理的请求会排空或取消，宿主接入配置保留。', '停止後此頁面將中斷連線，處理中的請求會排空或取消，宿主接入設定保留。', '停止するとこのページは切断されます。処理中のリクエストは終了または取消され、ホスト設定は保持されます。', 'Beim Stoppen wird diese Seite getrennt. Aktive Anfragen werden abgeschlossen oder abgebrochen; Host-Einstellungen bleiben erhalten.'],
    ['Restarting disconnects this page temporarily. The page will reconnect automatically.', '重启期间页面会暂时失联，随后将自动恢复连接。', '重新啟動期間頁面會暫時中斷，隨後將自動恢復連線。', '再起動中は一時的に切断されます。その後、自動的に再接続します。', 'Beim Neustart wird die Seite kurz getrennt und danach automatisch wieder verbunden.'],
    ['Confirm stop', '确认停止', '確認停止', '停止を確認', 'Stoppen bestätigen'],
    ['Confirm restart', '确认重启', '確認重新啟動', '再起動を確認', 'Neustart bestätigen'],
    ['Service controls are unavailable. Refresh or use the installed CLI.', '服务控制不可用，请刷新或使用已安装的命令行程序。', '服務控制無法使用，請重新整理或使用已安裝的命令列程式。', 'サービス操作を利用できません。更新するか、インストール済み CLI を使用してください。', 'Dienststeuerung nicht verfügbar. Aktualisieren oder die installierte CLI verwenden.'],
    ['Service operation failed. Refresh to inspect the current state.', '服务操作失败，请刷新确认当前状态。', '服務操作失敗，請重新整理確認目前狀態。', 'サービス操作に失敗しました。更新して現在の状態を確認してください。', 'Dienstvorgang fehlgeschlagen. Aktuellen Status durch Aktualisieren prüfen.'],
    ['Autostart setting saved.', '自启设置已保存。', '自動啟動設定已儲存。', '自動起動の設定を保存しました。', 'Autostart-Einstellung gespeichert.'],
    ['Stop requested. Start 端云协同 again using your usual launcher.', '已请求停止。需要时请通过原来的启动入口重新启动 端云协同。', '已要求停止。需要時請透過原來的啟動入口重新啟動 端云协同。', '停止を要求しました。通常の起動方法で 端云协同 を再起動してください。', 'Stoppen angefordert. 端云协同 bei Bedarf wie gewohnt erneut starten.'],
    ['Restart requested. Waiting for a new 端云协同 process…', '已请求重启，正在等待新的 端云协同 进程…', '已要求重新啟動，正在等候新的 端云协同 程序…', '再起動を要求しました。新しい 端云协同 プロセスを待っています…', 'Neustart angefordert. Auf neuen 端云协同-Prozess wird gewartet…'],
    ['端云协同 restarted. Management connection restored.', '端云协同 已重启，管理连接已自动恢复。', '端云协同 已重新啟動，管理連線已自動恢復。', '端云协同 が再起動し、管理接続を自動復元しました。', '端云协同 neu gestartet. Verwaltungsverbindung automatisch wiederhergestellt.'],
    ['Restart could not be confirmed. Start 端云协同 using your usual launcher, then refresh.', '尚未确认重启成功。请通过原来的启动入口启动 端云协同，然后刷新页面。', '尚未確認重新啟動成功。請透過原來的啟動入口啟動 端云协同，再重新整理頁面。', '再起動を確認できません。通常の起動方法で 端云协同 を起動してから、ページを更新してください。', 'Neustart nicht bestätigt. 端云协同 wie gewohnt starten und die Seite aktualisieren.'],
  ]) for (const [lang, text] of [['zh', zh], ['zh-TW', tw], ['ja', ja], ['de', de]]) (I18N[lang] ||= {})[key] = text;

  let working = false, confirmation = '', acknowledged = false, command = '', message = '', phase = '', timer, generation = 0;
  const active = () => healthy && managementToken && serviceInfo?.running === true;
  function row(name, value, id) {
    const node = el('div', 'row pref'), who = el('div', 'who');
    who.append(el('div', 'name', t(name))); node.append(who);
    const text = el('code', 'router-service-value', value); if (id) text.id = id; node.append(text); return node;
  }
  function button(name, id, action, disabled = false) {
    const node = el('button', 'text', t(name)); node.type = 'button'; node.id = id;
    node.disabled = working || disabled; node.onclick = action; return node;
  }
  function render() {
    const root = $('#serviceControl'); root.replaceChildren();
    root.append(el('div', 'row-head router-label', t('Service management')));
    if (active() && phase === 'stop') { phase = ''; message = ''; }
    if (active() && typeof serviceInfo.startCommand === 'string') command = serviceInfo.startCommand;
    const list = el('div', 'list prefs');
    list.append(row('Service', t(active() ? 'Running' : 'Connection unavailable'), 'serviceControlState'));
    if (active()) {
      list.append(row('Process ID', String(serviceInfo.pid || '—'), 'serviceControlPID'), row('Program location', serviceInfo.executable || '—', 'serviceControlExecutable'));
      list.append(row('Experimental redaction at startup', t(serviceInfo.experimentalRedaction ? 'Enabled' : 'Disabled'), 'serviceControlTrial'));
      const autostart = serviceInfo.autostart;
      const known = autostart?.supported === true && !autostart.error && typeof autostart.enabled === 'boolean';
      const auto = row('Start at login', t(autostart?.supported === false ? 'Autostart is unavailable on this platform.' : known ? autostart.enabled ? 'Enabled' : 'Disabled' : 'Autostart status is unknown. Refresh before changing it.'), 'serviceControlAutostartState');
      auto.append(button(known && autostart.enabled ? 'Disable autostart' : 'Enable autostart', 'serviceControlAutostart', () => action('autostart', { enabled: !autostart.enabled }), !known)); list.append(auto);
    }
    root.append(list);
    const actions = el('div', 'request-choices');
    for (const [name, actionName] of [['Stop 端云协同', 'stop'], ['Restart 端云协同', 'restart']]) {
      actions.append(button(name, 'serviceControl-' + actionName, () => { confirmation = actionName; acknowledged = false; render(); }, !active() || !command));
    }
    actions.append(button('Refresh now', 'serviceControlRefresh', () => refresh(), working)); root.append(actions);
    if (confirmation && active()) {
      const panel = el('div', 'router-service-confirm'); panel.id = 'serviceControlConfirm';
      panel.append(el('p', 'hint router-note', t(confirmation === 'stop' ? 'Stopping disconnects this page. Active requests will be drained or canceled. Host connections stay configured.' : 'Restarting disconnects this page temporarily. The page will reconnect automatically.')));
      const label = el('label', 'router-service-ack'), check = el('input'); check.type = 'checkbox'; check.id = 'serviceControlAck'; check.checked = acknowledged;
      check.onchange = () => { acknowledged = check.checked; $('#serviceControlExecute').disabled = working || !acknowledged; };
      label.append(check, el('span', '', t('I understand this will disconnect the page.'))); panel.append(label);
      const controls = el('div', 'request-choices');
      controls.append(button('Cancel', 'serviceControlCancel', () => { confirmation = ''; acknowledged = false; render(); }), button(confirmation === 'stop' ? 'Confirm stop' : 'Confirm restart', 'serviceControlExecute', () => action(confirmation), !acknowledged)); panel.append(controls); root.append(panel);
    }
    if (active() && !command) root.append(el('p', 'hint router-note', t('Service controls are unavailable. Refresh or use the installed CLI.')));
    const status = el('p', 'usage-note router-note', message ? t(message) : ''); status.id = 'serviceControlMessage'; status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite'); root.append(status);
  }
  async function request(path, method = 'GET', body, token = managementToken) {
    const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 3000);
    try {
      const send = path === '/healthz' ? fetch : managementFetch;
      return await send(path, { method, headers: { ...(token ? { Authorization: 'Bearer ' + token } : {}), ...(method === 'POST' && serviceInfo?.processStamp ? { 'X-Wefto-Instance': serviceInfo.processStamp } : {}), ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }), credentials: 'omit', cache: 'no-store', redirect: 'error', signal: controller.signal });
    } finally { clearTimeout(timeout); }
  }
  function clearPrivateState() {
    serviceInfo = null; managementToken = ''; healthy = false; renderService();
    window.routerAppsDisconnected?.(); window.routerProvidersDisconnected?.(); window.routerObservationsDisconnected?.(); window.routerLocalDisconnected?.();
  }
  async function awaitRestart(oldPID, attempt = 0, run = generation) {
    if (run !== generation) return;
    try {
      const response = await request('/healthz'); const data = await response.json();
      if (response.ok && isWeftoService(data.service) && data.status === 'ok' && Number.isInteger(data.pid) && data.pid > 0 && data.pid !== oldPID) {
        await refreshService();
        if (serviceInfo?.pid !== data.pid) throw new Error('Restart not ready');
        working = false; phase = ''; message = '端云协同 restarted. Management connection restored.';
        window.routerProvidersConnected?.(); window.routerAppsOpen?.(); window.routerObservationsOpen?.(); window.routerLocalOpen?.();
        renderService(); render(); return;
      }
    } catch {}
    if (run !== generation) return;
    if (attempt >= 14) { working = false; phase = ''; message = 'Restart could not be confirmed. Start 端云协同 using your usual launcher, then refresh.'; render(); return; }
    timer = setTimeout(() => awaitRestart(oldPID, attempt + 1, run), 1000);
  }
  async function action(name, body) {
    if (working || !active() || (name !== 'autostart' && (!acknowledged || confirmation !== name || !command))) return;
    const oldPID = serviceInfo.pid; working = true; message = ''; render();
    try {
      const response = await request('/api/service/' + name, 'POST', body || {});
      const result = await response.json();
      if (!response.ok) {
        if (response.status === 401) { clearPrivateState(); throw new Error('Invalid management token. Please try again.'); }
        throw new Error('Service operation failed. Refresh to inspect the current state.');
      }
      if (name === 'autostart') {
        if (!isWeftoService(result.service) || result.running !== true || result.autostart?.enabled !== body.enabled) throw new Error('Service operation failed. Refresh to inspect the current state.');
        serviceInfo = result; message = 'Autostart setting saved.';
      } else {
        if (result[name === 'stop' ? 'stopping' : 'restarting'] !== true) throw new Error('Service operation failed. Refresh to inspect the current state.');
        if (typeof result.startCommand === 'string' && result.startCommand) command = result.startCommand;
        confirmation = ''; acknowledged = false; phase = name;
        message = name === 'stop' ? 'Stop requested. Start 端云协同 again using your usual launcher.' : 'Restart requested. Waiting for a new 端云协同 process…';
        clearPrivateState();
        if (name === 'restart') { generation++; awaitRestart(oldPID); }
      }
    } catch (error) { message = error.message === 'Invalid management token. Please try again.' ? error.message : 'Service operation failed. Refresh to inspect the current state.'; }
    finally { if (phase !== 'restart') working = false; render(); }
  }
  window.routerServiceRender = render;
  window.routerServiceDisconnected = () => { confirmation = ''; acknowledged = false; render(); };
  window.addEventListener('pagehide', () => { generation++; clearTimeout(timer); });
  render();
})();
