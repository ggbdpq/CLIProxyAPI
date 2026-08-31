// ============================================================================
// CLIProxyAPI「数据管理」面板注入 —— content script（world: MAIN）
// 来源：internal/managementasset/data_management_extension.html 的 <script> 部分
// 同步：本文件与该 HTML 的 <script> 内容保持一致，修改时两处同步。
// 说明：world="MAIN" 使脚本运行在页面主世界，可读取管理面板已保存的管理密钥
//       （localStorage / sessionStorage 可见），同源 fetch 无 CORS 限制。
//       本脚本仅操作管理面板 DOM 与同源 /v0/management/* API，
//       不采集、不外发任何数据，密钥只通过页面本地存储与页面自身逻辑处理。
// 兼容：若同时启用了 Go //go:embed 注入，二者共享同一幂等标记
//       window.__cpaDataMgmtInjected，后注入者自动跳过，不会双重渲染。
// ============================================================================

if (window.__cpaDataMgmtInjected) {
  // 已通过其它途径（Go embed 注入 / 插件重复注入）注入过，跳过。
} else {
  window.__cpaDataMgmtInjected = true;

  // ---- 以下原样提取自 data_management_extension.html 的 <script> ----
  (function () {
    var PAGE_ID = 'cpa-data-management-page';
    var NAV_ID = 'cpa-data-management-nav';
    var active = false;
    var records = [];
    var total = 0;
    var message = '';
    var error = '';
    var loading = false;
    var selectedRecordIDs = Object.create(null);
    var pageSize = 50;
    var currentPage = 1;
    var searchQuery = '';
    var manualManagementKey = readManualManagementKey();

    function storageValues(storage) {
      var out = [];
      if (!storage) return out;
      for (var i = 0; i < storage.length; i++) {
        var key = storage.key(i);
        if (!key) continue;
        try { out.push([key, storage.getItem(key)]); } catch (_) {}
      }
      return out;
    }

    function readManualManagementKey() {
      try { return window.sessionStorage.getItem('cpa-data-management-key') || ''; } catch (_) { return ''; }
    }

    function setManualManagementKey(value) {
      manualManagementKey = String(value || '').trim();
      try {
        if (manualManagementKey) window.sessionStorage.setItem('cpa-data-management-key', manualManagementKey);
        else window.sessionStorage.removeItem('cpa-data-management-key');
      } catch (_) {}
    }

    function findManagementKey() {
      if (manualManagementKey && manualManagementKey.trim()) return manualManagementKey.trim();
      var stores = [window.localStorage, window.sessionStorage];
      for (var s = 0; s < stores.length; s++) {
        var values = storageValues(stores[s]);
        for (var i = 0; i < values.length; i++) {
          var raw = values[i][1];
          if (!raw) continue;
          try {
            var parsed = JSON.parse(raw);
            var candidates = [parsed && parsed.managementKey, parsed && parsed.state && parsed.state.managementKey, parsed && parsed.state && parsed.state.auth && parsed.state.auth.managementKey];
            for (var c = 0; c < candidates.length; c++) {
              if (typeof candidates[c] === 'string' && candidates[c].trim()) return candidates[c].trim();
            }
          } catch (_) {
            if (/management|key|token/i.test(values[i][0]) && raw.length < 512) return raw;
          }
        }
      }
      var input = document.querySelector('input[name="cpa-management-key"]');
      return input && input.value ? input.value.trim() : '';
    }

    function authHeaders(json) {
      var headers = json ? { 'Content-Type': 'application/json' } : {};
      var key = findManagementKey();
      if (key) headers.Authorization = 'Bearer ' + key;
      return headers;
    }

    async function apiFetch(url, options) {
      var key = findManagementKey();
      if (!key) throw new Error('缺少管理密钥，请输入管理密钥后重试');
      options = options || {};
      options.headers = Object.assign({}, options.headers || {}, authHeaders(false));
      var resp = await fetch(url, options);
      var text = await resp.text();
      var data = text ? JSON.parse(text) : {};
      if (!resp.ok) throw new Error(data.error || data.message || ('请求失败：' + resp.status));
      return data;
    }

    function ensureNav() {
      var aside = document.querySelector('aside.sidebar');
      if (!aside || document.getElementById(NAV_ID)) return;
      var section = document.createElement('div');
      section.className = 'nav-section cpa-data-section';
      section.innerHTML = '<div class="nav-section-title">数据</div><button id="' + NAV_ID + '" type="button" class="nav-item cpa-data-nav" aria-label="数据管理"><span class="nav-icon">▦</span><span>数据管理</span></button>';
      aside.appendChild(section);
      section.querySelector('button').addEventListener('click', function () { showDataPage(); });
    }

    function ensurePage() {
      var body = document.querySelector('.main-body') || document.body;
      var page = document.getElementById(PAGE_ID);
      if (page) return page;
      page = document.createElement('main');
      page.id = PAGE_ID;
      page.style.display = 'none';
      body.appendChild(page);
      return page;
    }

    function setMainHidden(hidden) {
      var body = document.querySelector('.main-body') || document.body;
      Array.prototype.forEach.call(body.children, function (child) {
        if (child.matches && child.matches('aside.sidebar')) return;
        if (child.id === PAGE_ID) return;
        if (hidden) {
          if (child.dataset.cpaOldDisplay === undefined) child.dataset.cpaOldDisplay = child.style.display || '';
          child.style.display = 'none';
        } else if (child.dataset.cpaOldDisplay !== undefined) {
          child.style.display = child.dataset.cpaOldDisplay;
          delete child.dataset.cpaOldDisplay;
        }
      });
    }

    function render() {
      var page = ensurePage();
      var totalPages = Math.max(1, Math.ceil(total / pageSize));
      var paginationHTML = totalPages > 1
        ? '<span class="cpa-data-pagination">' +
          '<button id="cpa-data-prev" class="cpa-data-page-btn" type="button"' + (currentPage <= 1 ? ' disabled' : '') + '>上一页</button>' +
          '<span class="cpa-data-page-info">' + currentPage + ' / ' + totalPages + '</span>' +
          '<button id="cpa-data-next" class="cpa-data-page-btn" type="button"' + (currentPage >= totalPages ? ' disabled' : '') + '>下一页</button>' +
          '</span>'
        : '';
      page.innerHTML = '<div class="cpa-data-page"><h1 class="cpa-data-page-title">数据管理</h1><p class="cpa-data-page-desc">导入 JSONL 文件后会同步写入本地 SQLite 数据库，并在下方列表查看。</p><div class="cpa-data-card"><div class="cpa-data-toolbar"><input id="cpa-data-management-key" class="cpa-data-key" type="password" placeholder="请输入管理密钥" autocomplete="current-password" value="' + escapeHTML(manualManagementKey) + '" /><input id="cpa-data-file" class="cpa-data-file" type="file" accept=".jsonl,application/jsonl,text/plain" /><button id="cpa-data-import" class="cpa-data-btn" type="button">导入 JSONL</button><button id="cpa-data-export" class="cpa-data-btn" type="button">导出 JSONL</button><button id="cpa-data-refresh" class="cpa-data-btn" type="button">刷新列表</button><input id="cpa-data-search" class="cpa-data-search" type="search" placeholder="搜索 email、status 或 nextTime" value="' + escapeHTML(searchQuery) + '" /><button id="cpa-data-search-btn" class="cpa-data-btn" type="button">搜索</button>' + paginationHTML + '<button id="cpa-data-delete-selected" class="cpa-data-btn cpa-data-btn-danger" type="button">删除选中</button><button id="cpa-data-delete-all" class="cpa-data-btn cpa-data-btn-danger" type="button">全部删除</button><button id="cpa-data-generate-quota" class="cpa-data-btn" type="button">生成配额</button><span class="cpa-data-meta">共 ' + total + ' 条，已选 ' + selectedDataRecordIDs().length + ' 条</span></div><div class="cpa-data-message ' + (error ? 'error' : '') + '">' + escapeHTML(error || message || (loading ? '加载中...' : '')) + '</div><div class="cpa-data-table-wrap">' + tableHTML() + '</div></div></div>';
      page.querySelector('#cpa-data-import').disabled = loading;
      page.querySelector('#cpa-data-export').disabled = loading || selectedDataRecordIDs().length === 0;
      page.querySelector('#cpa-data-refresh').disabled = loading;
      page.querySelector('#cpa-data-delete-selected').disabled = loading || selectedDataRecordIDs().length === 0;
      page.querySelector('#cpa-data-delete-all').disabled = loading || total === 0;
      page.querySelector('#cpa-data-generate-quota').disabled = loading || selectedDataRecordIDs().length === 0;
      page.querySelector('#cpa-data-search-btn').disabled = loading;
      page.querySelector('#cpa-data-management-key').addEventListener('input', function (event) { setManualManagementKey(event.target.value); });
      page.querySelector('#cpa-data-search').addEventListener('keydown', function (event) { if (event.key === 'Enter') searchDataRecords(); });
      page.querySelector('#cpa-data-import').addEventListener('click', importFile);
      page.querySelector('#cpa-data-export').addEventListener('click', exportJSONL);
      page.querySelector('#cpa-data-refresh').addEventListener('click', function () { currentPage = 1; loadRecords(); });
      page.querySelector('#cpa-data-search-btn').addEventListener('click', searchDataRecords);
      var prevBtn = page.querySelector('#cpa-data-prev');
      var nextBtn = page.querySelector('#cpa-data-next');
      if (prevBtn) prevBtn.addEventListener('click', function () { goToPage(-1); });
      if (nextBtn) nextBtn.addEventListener('click', function () { goToPage(1); });
      page.querySelector('#cpa-data-delete-selected').addEventListener('click', function () { deleteSelectedRecords(); });
      page.querySelector('#cpa-data-delete-all').addEventListener('click', function () { deleteAllRecords(); });
      page.querySelector('#cpa-data-generate-quota').addEventListener('click', function () { generateQuotaFiles(); });
      page.querySelector('.cpa-data-table-wrap').addEventListener('click', function (event) {
        var deleteButton = event.target && event.target.closest ? event.target.closest('.cpa-data-row-delete') : null;
        if (deleteButton) {
          selectedRecordIDs = Object.create(null);
          selectedRecordIDs[String(deleteButton.getAttribute('data-id') || '')] = true;
          deleteSelectedRecords();
        }
      });
      page.querySelector('.cpa-data-table-wrap').addEventListener('change', function (event) {
        var selectAll = event.target && event.target.closest ? event.target.closest('.cpa-data-select-all') : null;
        if (selectAll) { toggleAllDataRecords(selectAll.checked); return; }
        var checkbox = event.target && event.target.closest ? event.target.closest('.cpa-data-row-select') : null;
        if (!checkbox) return;
        if (checkbox.checked) selectedRecordIDs[String(checkbox.value)] = true;
        else delete selectedRecordIDs[String(checkbox.value)];
        render();
      });
      syncSelectAllCheckbox();
    }


    function tableHTML() {
      if (!records.length) return '<div class="cpa-data-empty">暂无数据</div>';
      var columns = dataColumns(records);
      var hasQuota = columns.indexOf('quota') !== -1;
      var hasStatus = columns.indexOf('status') !== -1;
      var hasNextTime = columns.indexOf('nextTime') !== -1;
      var normalColumns = columns.filter(function (key) { return key !== 'quota' && key !== 'status' && key !== 'nextTime'; });
      var header = '<tr><th><label class="cpa-data-select-all-label"><input class="cpa-data-select-all" type="checkbox" aria-label="全选当前列表" title="全选当前列表" />选择</label></th><th>ID</th>' + (hasQuota ? '<th>额度</th>' : '') + (hasStatus ? '<th>status</th>' : '') + (hasNextTime ? '<th>nextTime</th>' : '') + normalColumns.map(function (key) { return '<th>' + escapeHTML(key) + '</th>'; }).join('') + '<th>操作</th></tr>';
      var rows = records.map(function (r) {
        var id = String(r.id);
        var data = (r.data && typeof r.data === 'object' && !Array.isArray(r.data)) ? r.data : { value: r.data };
        return '<tr>'
          + '<td><input class="cpa-data-row-select" type="checkbox" value="' + escapeHTML(id) + '"' + (selectedRecordIDs[id] ? ' checked' : '') + ' /></td>'
          + '<td>' + cellHTML(r.id) + '</td>'
          + (hasQuota ? '<td>' + cellHTML(data.quota, 'quota') + '</td>' : '')
          + (hasStatus ? '<td>' + cellHTML(data.status, 'status') + '</td>' : '')
          + (hasNextTime ? '<td>' + cellHTML(data.nextTime, 'nextTime') + '</td>' : '')
          + normalColumns.map(function (key) { return '<td>' + cellHTML(data[key], key) + '</td>'; }).join('')
          + '<td><button class="cpa-data-copy cpa-data-row-delete" type="button" data-id="' + escapeHTML(id) + '">删除</button></td>'
          + '</tr>';
      }).join('');
      var minWidth = Math.max(860, 260 + columns.length * 190);
      return '<table class="cpa-data-table" style="min-width:' + minWidth + 'px"><thead>' + header + '</thead><tbody>' + rows + '</tbody></table>';
    }

    function cellHTML(value, key) {
      var text = formatCellValue(value, key);
      return '<div class="cpa-data-cell"><pre class="cpa-data-cell-text">' + escapeHTML(text) + '</pre></div>';
    }

    function dataColumns(items) {
      var seen = Object.create(null);
      var columns = [];
      items.forEach(function (record) {
        var data = record && record.data;
        if (!data || typeof data !== 'object' || Array.isArray(data)) {
          if (!seen.value) { seen.value = true; columns.push('value'); }
          return;
        }
        Object.keys(data).forEach(function (key) {
          if (!seen[key]) { seen[key] = true; columns.push(key); }
        });
      });
      return prioritizeNextTimeColumn(columns);
    }

    function prioritizeNextTimeColumn(columns) {
      var priority = [];
      if (columns.indexOf('quota') !== -1) priority.push('quota');
      if (columns.indexOf('status') !== -1) priority.push('status');
      if (columns.indexOf('nextTime') !== -1) priority.push('nextTime');
      if (!priority.length) return columns;
      return priority.concat(columns.filter(function (key) { return key !== 'quota' && key !== 'status' && key !== 'nextTime'; }));
    }

    function formatCellValue(value, key) {
      if (value === undefined || value === null) return '';
      if (key === 'quota') return formatQuotaValue(value);
      if (key === 'nextTime') return formatNextTimeValue(value);
      if (typeof value === 'object') return JSON.stringify(value, null, 2);
      return String(value);
    }

    function formatQuotaValue(value) {
      var text = String(value).trim();
      if (!text) return '';
      if (/%$/.test(text)) return text;
      var number = Number(text);
      return Number.isFinite(number) ? String(number) + '%' : text;
    }

    function formatNextTimeValue(value) {
      var text = String(value).trim();
      if (!text) return '';
      if (/^\d{1,2}-\d{1,2}$/.test(text)) return text + ' 00:00';
      if (/^\d{1,2}-\d{1,2}\s+\d{1,2}:\d{1,2}$/.test(text)) {
        return text.replace(/(\d{1,2}):(\d{1,2})$/, function (_, hour, minute) { return pad2(hour) + ':' + pad2(minute); });
      }
      return text;
    }

    function pad2(value) {
      value = String(value);
      return value.length === 1 ? '0' + value : value;
    }

    function accountSummary(record) {
      var data = record.data || {};
      var summary = record.summary || {};
      var mailboxEnabled = summary.mailbox_enabled;
      var mailboxProvider = summary.mailbox_provider || (data.mailbox && data.mailbox.provider) || '-';
      return {
        platform: summary.platform || data.platform || '-',
        email: summary.email || data.email || summary.login_identity || data.login_identity || '-',
        status: summary.status || data.status || '-',
        orgProject: compactJoin([summary.organization_id || data.organization_id, summary.project_id || data.project_id], ' / ') || '-',
        usedAt: formatUnixTime(summary.last_used || data.last_used || summary.created_at || data.created_at),
        mailbox: mailboxProvider + ' · ' + (mailboxEnabled === true ? '启用' : '未启用')
      };
    }

    function compactJoin(values, sep) {
      return values.filter(function (v) { return v !== undefined && v !== null && String(v).trim() !== ''; }).join(sep);
    }

    function formatUnixTime(value) {
      if (value === undefined || value === null || value === '') return '-';
      var number = Number(value);
      if (!Number.isFinite(number)) return String(value);
      if (number > 100000000000) number = Math.floor(number / 1000);
      var date = new Date(number * 1000);
      if (Number.isNaN(date.getTime())) return String(value);
      return date.toLocaleString();
    }

    function escapeHTML(value) {
      return String(value == null ? '' : value).replace(/[&<>"]/g, function (ch) { return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[ch]; });
    }

    function selectedDataRecordIDs() {
      return Object.keys(selectedRecordIDs).map(function (id) { return Number.parseInt(id, 10); }).filter(function (id) { return Number.isSafeInteger(id) && id > 0; });
    }

    function toggleAllDataRecords(checked) {
      selectedRecordIDs = Object.create(null);
      if (checked) {
        selectableDataRecordIDs().forEach(function (id) { selectedRecordIDs[id] = true; });
      }
      render();
    }

    function selectableDataRecordIDs() {
      return records.map(function (record) { return String(record.id); }).filter(function (id) {
        var parsed = Number.parseInt(id, 10);
        return Number.isSafeInteger(parsed) && parsed > 0;
      });
    }

    function syncSelectAllCheckbox() {
      var checkbox = document.querySelector('#' + PAGE_ID + ' .cpa-data-select-all');
      if (!checkbox) return;
      var selectableIDs = selectableDataRecordIDs();
      var selectedCount = selectableIDs.filter(function (id) { return selectedRecordIDs[id]; }).length;
      checkbox.checked = selectableIDs.length > 0 && selectedCount === selectableIDs.length;
      checkbox.indeterminate = selectedCount > 0 && selectedCount < selectableIDs.length;
    }

    function pruneSelectedRecordIDs() {
      var keep = Object.create(null);
      records.forEach(function (record) {
        var id = String(record.id);
        if (selectedRecordIDs[id]) keep[id] = true;
      });
      selectedRecordIDs = keep;
    }

    async function loadRecords() {
      loading = true; error = ''; message = '正在读取 SQLite 数据...'; render();
      try {
        var offset = (currentPage - 1) * pageSize;
        var data = await apiFetch('/v0/management/data-records?limit=' + pageSize + '&offset=' + offset + (searchQuery ? '&q=' + encodeURIComponent(searchQuery) : ''));
        records = data.records || [];
        total = data.total || 0;
        if (records.length === 0 && currentPage > 1) { currentPage = 1; loading = false; render(); return; }
        message = records.length ? '列表已更新' : '暂无导入数据';
      } catch (e) { error = e.message || String(e); }
      loading = false; render();
    }

    function goToPage(delta) {
      currentPage = Math.max(1, currentPage + delta);
      loadRecords();
    }

    function searchDataRecords() {
      var input = document.getElementById('cpa-data-search');
      searchQuery = input && input.value ? input.value.trim() : '';
      currentPage = 1;
      selectedRecordIDs = Object.create(null);
      loadRecords();
    }

    async function deleteSelectedRecords() {
      var ids = selectedDataRecordIDs();
      if (!ids.length) { error = '请选择要删除的数据'; message = ''; render(); return; }
      if (!window.confirm('确定删除选中的 ' + ids.length + ' 条数据吗？')) return;
      loading = true; error = ''; message = '正在删除选中数据...'; render();
      try {
        var data = await apiFetch('/v0/management/data-records', { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: ids }) });
        selectedRecordIDs = Object.create(null);
        message = '已删除 ' + (data.deleted || 0) + ' 条数据';
        await loadRecords();
      } catch (e) { error = e.message || String(e); loading = false; render(); }
    }

    async function deleteAllRecords() {
      if (!total) { error = '暂无数据可删除'; message = ''; render(); return; }
      if (!window.confirm('确定删除全部 ' + total + ' 条数据吗？此操作不可恢复。')) return;
      loading = true; error = ''; message = '正在删除全部数据...'; render();
      try {
        var data = await apiFetch('/v0/management/data-records', { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ all: true }) });
        selectedRecordIDs = Object.create(null);
        currentPage = 1;
        message = '已删除 ' + (data.deleted || 0) + ' 条数据';
        await loadRecords();
      } catch (e) { error = e.message || String(e); loading = false; render(); }
    }


    function showQuotaResultDialog(exported, files, outputDir) {
      var dialog = document.getElementById('cpa-quota-result-dialog');
      if (!dialog) {
        dialog = document.createElement('dialog');
        dialog.id = 'cpa-quota-result-dialog';
        dialog.className = 'cpa-quota-result';
        document.body.appendChild(dialog);
      }
      var names = files || [];
      var list = names.map(function (name) { return '<li>' + escapeHTML(name) + '</li>'; }).join('');
      dialog.innerHTML = '<h2>配额文件已生成</h2><p>已生成 ' + Number(exported || 0) + ' 条配额文件' + (outputDir ? '到 ' + escapeHTML(outputDir) : '') + '。</p>'
        + (list ? '<details><summary>查看详情</summary><ul class="cpa-quota-result-list">' + list + '</ul></details>' : '')
        + '<div class="cpa-quota-result-actions"><button type="button" class="cpa-data-btn" id="cpa-quota-result-close">关闭</button></div>';
      dialog.querySelector('#cpa-quota-result-close').addEventListener('click', function () { dialog.close(); });
      if (dialog.showModal) dialog.showModal(); else dialog.setAttribute('open', '');
    }

    function enhanceQuotaCards() {
      var cards = document.querySelectorAll('[class*="QuotaCard-module__card___"]');
      Array.prototype.forEach.call(cards, function (card) {
        if (card.querySelector('.cpa-quota-card-delete')) return;
        var nameEl = card.querySelector('[class*="QuotaCard-module__fileName"]');
        var name = nameEl && String(nameEl.textContent || '').trim();
        if (!name) return;
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'cpa-quota-card-delete';
        btn.textContent = '删除';
        btn.setAttribute('data-auth-file', name);
        btn.addEventListener('click', function (event) {
          event.preventDefault();
          event.stopPropagation();
          deleteQuotaAuthFile(name, card);
        });
        card.appendChild(btn);
      });
    }

    async function deleteQuotaAuthFile(name, card) {
      if (!window.confirm('确定删除凭证 ' + name + ' 吗？')) return;
      try {
        await apiFetch('/v0/management/auth-files', { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: name }) });
        window.dispatchEvent(new Event('auth-files-changed'));
        if (card && card.parentNode) card.parentNode.removeChild(card);
      } catch (e) {
        window.alert(e.message || String(e));
      }
    }

    async function generateQuotaFiles() {
      var ids = selectedDataRecordIDs();
      if (!ids.length) { error = '请选择要生成配额的数据'; message = ''; render(); return; }
      loading = true; error = ''; message = '正在生成配额文件...'; render();
      try {
        var data = await apiFetch('/v0/management/data-records/generate-quota', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: ids }) });
        message = '已生成 ' + (data.exported || 0) + ' 条配额文件';
        loading = false; render();
        showQuotaResultDialog(data.exported, data.files, data.output_dir);
      } catch (e) { error = e.message || String(e); loading = false; render(); }
    }

    async function exportJSONL() {
      var ids = selectedDataRecordIDs();
      if (!ids.length) { error = '请选择要导出的数据'; message = ''; render(); return; }
      loading = true; error = ''; message = '正在导出 JSONL...'; render();
      try {
        var url = '/v0/management/data-records/export?ids=' + ids.join(',');
        var key = findManagementKey();
        if (!key) throw new Error('缺少管理密钥，请输入管理密钥后重试');
        var resp = await fetch(url, { headers: { Authorization: 'Bearer ' + key } });
        if (!resp.ok) {
          var text = await resp.text();
          var data = text ? JSON.parse(text) : {};
          throw new Error(data.error || data.message || ('请求失败：' + resp.status));
        }
        var blob = await resp.blob();
        var anchor = document.createElement('a');
        anchor.href = URL.createObjectURL(blob);
        anchor.download = 'data-records.jsonl';
        document.body.appendChild(anchor);
        anchor.click();
        document.body.removeChild(anchor);
        URL.revokeObjectURL(anchor.href);
        message = '已导出 ' + ids.length + ' 条数据';
      } catch (e) { error = e.message || String(e); }
      loading = false; render();
    }

    async function importFile() {
      var input = document.getElementById('cpa-data-file');
      var file = input && input.files && input.files[0];
      if (!file) { error = '请选择 .jsonl 文件'; message = ''; render(); return; }
      var form = new FormData();
      form.append('file', file);
      loading = true; error = ''; message = '正在导入并写入 SQLite...'; render();
      try {
        var data = await apiFetch('/v0/management/data-records/import', { method: 'POST', body: form });
        message = '导入成功：' + (data.imported || 0) + ' 条';
        currentPage = 1;
        await loadRecords();
      } catch (e) { error = e.message || String(e); loading = false; render(); }
    }

    function setDataNavActive(isActive) {
      var nav = document.getElementById(NAV_ID);
      if (!nav) return;
      if (isActive) {
        Array.prototype.forEach.call(document.querySelectorAll('aside.sidebar .nav-item.active'), function (item) {
          if (item !== nav) item.classList.remove('active');
        });
        nav.classList.add('active');
        nav.classList.add('cpa-active');
        nav.setAttribute('aria-current', 'page');
        return;
      }
      nav.classList.remove('active');
      nav.classList.remove('cpa-active');
      nav.removeAttribute('aria-current');
    }

    function showDataPage() {
      active = true;
      if (location.hash !== '#/data-management') history.pushState(null, '', '#/data-management');
      ensureNav(); ensurePage(); setMainHidden(true);
      var page = document.getElementById(PAGE_ID);
      if (page) page.style.display = 'block';
      setDataNavActive(true);
      render();
      if (findManagementKey()) loadRecords();
      else { error = '请输入管理密钥后再导入 JSONL'; message = ''; render(); }
    }

    function hideDataPage() {
      active = false;
      setMainHidden(false);
      var page = document.getElementById(PAGE_ID);
      if (page) page.style.display = 'none';
      setDataNavActive(false);
    }

    document.addEventListener('click', function (event) {
      var nav = event.target && event.target.closest ? event.target.closest('aside.sidebar .nav-item') : null;
      if (nav && nav.id !== NAV_ID) hideDataPage();
    }, true);

    window.addEventListener('popstate', function () {
      if (location.hash === '#/data-management') showDataPage(); else hideDataPage();
    });
    new MutationObserver(function () {
      ensureNav();
      enhanceQuotaCards();
      if (active) { ensurePage(); setMainHidden(true); setDataNavActive(true); }
    }).observe(document.documentElement, { childList: true, subtree: true });
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', function () { ensureNav(); enhanceQuotaCards(); }); else { ensureNav(); enhanceQuotaCards(); }
    if (location.hash === '#/data-management') setTimeout(showDataPage, 0);
  })();
}
