import './style.css';

import {
    AddDownload,
    CancelDownload,
    GetConfig,
    ListDownloads,
    OpenPath,
    PauseDownload,
    PickDownloadDir,
    RemoveDownload,
    ResumeDownload,
    RevealFile,
    SetConfig,
} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';

const STATUS_LABELS = {
    queued: 'Queued',
    active: 'Downloading',
    paused: 'Paused',
    completed: 'Completed',
    failed: 'Failed',
    canceled: 'Canceled',
};

let downloads = [];
let config = null;
let currentFilter = 'all';

const app = document.querySelector('#app');

app.innerHTML = `
    <header class="topbar">
        <div class="topbar-actions">
            <button id="settings-btn" class="btn btn-ghost icon-only" title="Settings" aria-label="Settings">⚙</button>
            <button id="add-btn" class="btn btn-primary icon-only" title="Add download" aria-label="Add download">+</button>
        </div>
    </header>

    <div class="toolbar">
        <div class="filters" id="filters">
            <button class="filter active" data-filter="all">All</button>
            <button class="filter" data-filter="active">Downloading</button>
            <button class="filter" data-filter="queued">Queued</button>
            <button class="filter" data-filter="completed">Completed</button>
            <button class="filter" data-filter="failed">Failed</button>
        </div>
        <div class="summary" id="summary"></div>
    </div>

    <main class="content">
        <div id="list" class="list"></div>
        <div id="empty" class="empty hidden">
            <div class="empty-icon">↓</div>
            <h2>Nothing here yet</h2>
            <p>Paste a link to start your first download.</p>
            <button class="btn btn-primary" id="empty-add">+ Add download</button>
        </div>
    </main>

    <div id="toasts" class="toasts"></div>

    <div id="add-modal" class="modal hidden">
        <div class="modal-backdrop" data-close="add"></div>
        <div class="modal-card">
            <div class="modal-head">
                <h2>Add download</h2>
                <button class="icon-btn" data-close="add" title="Close">✕</button>
            </div>
            <form id="add-form">
                <label for="url">URL</label>
                <input id="url" type="url" placeholder="https://example.com/file.zip" required autocomplete="off"/>

                <details class="advanced">
                    <summary>Advanced options</summary>
                    <div class="field-row">
                        <div>
                            <label for="algo">Checksum algorithm</label>
                            <select id="algo">
                                <option value="">None</option>
                                <option value="sha256">SHA-256</option>
                                <option value="sha1">SHA-1</option>
                                <option value="md5">MD5</option>
                            </select>
                        </div>
                        <div class="grow">
                            <label for="checksum">Expected hash</label>
                            <input id="checksum" type="text" placeholder="optional" autocomplete="off"/>
                        </div>
                    </div>
                </details>

                <div class="modal-actions">
                    <button type="button" class="btn btn-ghost" data-close="add">Cancel</button>
                    <button type="submit" class="btn btn-primary">Start</button>
                </div>
                <div id="add-error" class="form-error"></div>
            </form>
        </div>
    </div>

    <div id="settings-modal" class="modal hidden">
        <div class="modal-backdrop" data-close="settings"></div>
        <div class="modal-card">
            <div class="modal-head">
                <h2>Settings</h2>
                <button class="icon-btn" data-close="settings" title="Close">✕</button>
            </div>
            <form id="settings-form">
                <label for="download-dir">Download folder</label>
                <div class="folder-row">
                    <input id="download-dir" type="text" readonly/>
                    <button type="button" class="btn btn-ghost" id="pick-dir">Browse</button>
                </div>

                <div class="field-row">
                    <div>
                        <label for="max-downloads">Concurrent downloads</label>
                        <input id="max-downloads" type="number" min="1" max="10"/>
                    </div>
                    <div>
                        <label for="max-conns">Connections per download</label>
                        <input id="max-conns" type="number" min="1" max="32"/>
                    </div>
                </div>

                <div class="field-row">
                    <div>
                        <label for="speed-limit">Speed limit (MB/s, 0 = unlimited)</label>
                        <input id="speed-limit" type="number" min="0" step="0.5"/>
                    </div>
                    <div>
                        <label for="retries">Retry attempts</label>
                        <input id="retries" type="number" min="1" max="20"/>
                    </div>
                </div>

                <div class="modal-actions">
                    <button type="button" class="btn btn-ghost" data-close="settings">Cancel</button>
                    <button type="submit" class="btn btn-primary">Save</button>
                </div>
                <div id="settings-error" class="form-error"></div>
            </form>
        </div>
    </div>
`;

const $ = (selector) => document.querySelector(selector);
const listEl = $('#list');
const emptyEl = $('#empty');
const summaryEl = $('#summary');

function esc(value) {
    return String(value ?? '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
}

function formatBytes(bytes) {
    if (!Number.isFinite(bytes) || bytes < 0) return '—';
    if (bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
    const value = bytes / Math.pow(1024, i);
    return `${value.toFixed(i === 0 ? 0 : value >= 100 ? 0 : 1)} ${units[i]}`;
}

function formatSpeed(bytesPerSecond) {
    if (!bytesPerSecond || bytesPerSecond <= 0) return '0 B/s';
    return `${formatBytes(bytesPerSecond)}/s`;
}

function formatETA(seconds) {
    if (seconds == null || seconds < 0 || !Number.isFinite(seconds)) return '—';
    if (seconds < 60) return `${Math.ceil(seconds)}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.ceil(seconds % 60)}s`;
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}

function percent(d) {
    if (!d.totalSize || d.totalSize <= 0) return 0;
    return Math.min(100, Math.max(0, (d.downloaded / d.totalSize) * 100));
}

function isFinished(d) {
    return d.status === 'completed';
}

function isActive(d) {
    return d.status === 'active';
}

function renderList() {
    const visible = downloads.filter((d) => currentFilter === 'all' || d.status === currentFilter);

    if (visible.length === 0) {
        listEl.innerHTML = '';
        emptyEl.classList.remove('hidden');
        $('#empty-add').onclick = () => openModal('add');
        return;
    }
    emptyEl.classList.add('hidden');

    listEl.innerHTML = visible.map((d) => {
        const pct = percent(d);
        const finished = isFinished(d);
        const active = isActive(d);
        const canResume = ['paused', 'failed', 'canceled'].includes(d.status);
        const statusClass = `status status-${esc(d.status)}`;

        return `
            <article class="card" data-id="${esc(d.id)}">
                <div class="card-head">
                    <div class="file-icon ${finished ? 'done' : ''}">${finished ? '✓' : '↓'}</div>
                    <div class="file-meta">
                        <div class="file-name" title="${esc(d.name || d.url)}">${esc(d.name || d.url)}</div>
                        <div class="file-sub">
                            <span class="${statusClass}">${STATUS_LABELS[d.status] || esc(d.status)}</span>
                            <span class="dot">·</span>
                            <span class="size-label" data-size-for="${esc(d.id)}">${formatBytes(d.downloaded)} / ${formatBytes(d.totalSize)}</span>
                        </div>
                    </div>
                    <div class="card-actions">
                        ${active ? `<button class="icon-btn" data-action="pause" title="Pause">⏸</button>` : ''}
                        ${canResume ? `<button class="icon-btn" data-action="resume" title="Resume">▶</button>` : ''}
                        ${d.status === 'queued' ? `<button class="icon-btn" data-action="pause" title="Pause">⏸</button>` : ''}
                        ${!finished ? `<button class="icon-btn danger" data-action="cancel" title="Cancel">✕</button>` : ''}
                        ${finished ? `<button class="icon-btn" data-action="open" title="Open file">↗</button>` : ''}
                        ${finished ? `<button class="icon-btn" data-action="reveal" title="Show in folder">📁</button>` : ''}
                        <button class="icon-btn danger" data-action="remove" title="Remove from list">🗑</button>
                    </div>
                </div>

                ${finished ? '' : `
                    <div class="progress-wrap">
                        <div class="progress">
                            <div class="progress-bar" data-progress-for="${esc(d.id)}" style="width:${pct}%"></div>
                        </div>
                        <span class="pct" data-pct-for="${esc(d.id)}">${pct.toFixed(1)}%</span>
                    </div>
                    <div class="card-foot">
                        <span data-speed-for="${esc(d.id)}">${active ? '—' : ''}</span>
                        <span data-eta-for="${esc(d.id)}"></span>
                    </div>
                `}

                ${d.error ? `<div class="error-line">${esc(d.error)}</div>` : ''}
            </article>
        `;
    }).join('');
}

function updateSummary() {
    const active = downloads.filter((d) => d.status === 'active').length;
    const done = downloads.filter((d) => d.status === 'completed').length;
    summaryEl.innerHTML = `
        <span><strong>${active}</strong> active</span>
        <span class="dot">·</span>
        <span><strong>${done}</strong> completed</span>
        <span class="dot">·</span>
        <span><strong>${downloads.length}</strong> total</span>
    `;
}

function updateProgress(ev) {
    const bar = document.querySelector(`[data-progress-for="${CSS.escape(ev.id)}"]`);
    if (!bar) return;
    const pct = ev.total > 0 ? Math.min(100, (ev.downloaded / ev.total) * 100) : 0;
    bar.style.width = `${pct}%`;
    const pctEl = document.querySelector(`[data-pct-for="${CSS.escape(ev.id)}"]`);
    if (pctEl) pctEl.textContent = `${pct.toFixed(1)}%`;
    const sizeEl = document.querySelector(`[data-size-for="${CSS.escape(ev.id)}"]`);
    if (sizeEl) sizeEl.textContent = `${formatBytes(ev.downloaded)} / ${formatBytes(ev.total)}`;
    const speedEl = document.querySelector(`[data-speed-for="${CSS.escape(ev.id)}"]`);
    if (speedEl) speedEl.textContent = formatSpeed(ev.speed);
    const etaEl = document.querySelector(`[data-eta-for="${CSS.escape(ev.id)}"]`);
    if (etaEl) etaEl.textContent = ev.eta >= 0 ? `ETA ${formatETA(ev.eta)}` : '';
}

function upsertDownload(d) {
    const idx = downloads.findIndex((item) => item.id === d.id);
    if (idx >= 0) downloads[idx] = d;
    else downloads.unshift(d);
    renderList();
    updateSummary();
}

function toast(message, kind = 'info') {
    const el = document.createElement('div');
    el.className = `toast toast-${kind}`;
    el.textContent = message;
    $('#toasts').appendChild(el);
    setTimeout(() => el.remove(), 4500);
}

async function run(action, ...args) {
    try {
        await action(...args);
    } catch (err) {
        toast(`Error: ${err}`, 'error');
    }
}

function openModal(name) {
    $(`#${name}-modal`).classList.remove('hidden');
    if (name === 'add') setTimeout(() => $('#url').focus(), 30);
}

function closeModal(name) {
    $(`#${name}-modal`).classList.add('hidden');
}

async function loadConfig() {
    try {
        config = await GetConfig();
        $('#download-dir').value = config.downloadDir || '';
        $('#max-downloads').value = config.maxConcurrentDownloads;
        $('#max-conns').value = config.maxConnectionsPerDownload;
        $('#speed-limit').value = config.globalSpeedLimit ? (config.globalSpeedLimit / (1024 * 1024)).toFixed(1) : 0;
        $('#retries').value = config.retryMaxAttempts;
    } catch (err) {
        toast(`Could not load settings: ${err}`, 'error');
    }
}

async function refresh() {
    try {
        downloads = await ListDownloads();
        renderList();
        updateSummary();
    } catch (err) {
        toast(`Could not load downloads: ${err}`, 'error');
    }
}

// --- Events from Go ---
EventsOn('download:state', (ev) => upsertDownload(ev.download));
EventsOn('download:progress', (ev) => updateProgress(ev));
EventsOn('download:complete', (ev) => toast(`Finished: ${ev.name}`, 'success'));
EventsOn('download:error', (ev) => toast(`Failed: ${ev.name} — ${ev.error}`, 'error'));

// --- UI wiring ---
$('#add-btn').onclick = () => openModal('add');
$('#settings-btn').onclick = () => openModal('settings');

document.addEventListener('click', (ev) => {
    const closeTarget = ev.target.closest('[data-close]');
    if (closeTarget) {
        closeModal(closeTarget.dataset.close);
        return;
    }
    const filterBtn = ev.target.closest('[data-filter]');
    if (filterBtn) {
        currentFilter = filterBtn.dataset.filter;
        document.querySelectorAll('[data-filter]').forEach((el) => {
            el.classList.toggle('active', el === filterBtn);
        });
        renderList();
    }
});

$('#add-form').addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const url = $('#url').value.trim();
    const algo = $('#algo').value;
    const hash = $('#checksum').value.trim();
    if (!url) return;
    $('#add-error').textContent = '';
    try {
        await AddDownload(url, algo, hash);
        $('#url').value = '';
        $('#checksum').value = '';
        closeModal('add');
        toast('Download added');
    } catch (err) {
        $('#add-error').textContent = String(err);
    }
});

listEl.addEventListener('click', async (ev) => {
    const button = ev.target.closest('[data-action]');
    if (!button) return;
    const card = button.closest('[data-id]');
    const id = card.dataset.id;
    const item = downloads.find((d) => d.id === id);
    if (!item) return;

    switch (button.dataset.action) {
        case 'pause':
            await run(PauseDownload, id);
            break;
        case 'resume':
            await run(ResumeDownload, id);
            break;
        case 'cancel':
            await run(CancelDownload, id);
            break;
        case 'open':
            await run(OpenPath, item.finalPath);
            break;
        case 'reveal':
            await run(RevealFile, item.finalPath);
            break;
        case 'remove': {
            const deleteFile = item.status === 'completed'
                ? window.confirm('Delete the downloaded file from disk as well?')
                : false;
            await run(RemoveDownload, id, deleteFile);
            break;
        }
    }
});

$('#pick-dir').onclick = async () => {
    try {
        const dir = await PickDownloadDir();
        if (dir) $('#download-dir').value = dir;
    } catch (err) {
        toast(`Could not choose folder: ${err}`, 'error');
    }
};

$('#settings-form').addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const speedMB = parseFloat($('#speed-limit').value) || 0;
    const next = {
        ...config,
        downloadDir: $('#download-dir').value.trim(),
        maxConcurrentDownloads: parseInt($('#max-downloads').value, 10) || 1,
        maxConnectionsPerDownload: parseInt($('#max-conns').value, 10) || 1,
        globalSpeedLimit: Math.round(speedMB * 1024 * 1024),
        retryMaxAttempts: parseInt($('#retries').value, 10) || 1,
    };
    try {
        await SetConfig(next);
        config = next;
        closeModal('settings');
        toast('Settings saved');
    } catch (err) {
        $('#settings-error').textContent = String(err);
    }
});

document.addEventListener('keydown', (ev) => {
    if (ev.key === 'Escape') {
        closeModal('add');
        closeModal('settings');
    }
});

await loadConfig();
await refresh();
