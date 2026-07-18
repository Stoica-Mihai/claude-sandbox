// Share tunnel UI: globe indicator + SHARE modal, driven by /api/share/*.
// No SSE — status is fetched on page load and after each action (the wrapper
// answers mutations with the final state).

const SHARE_HINT = 'Tunnel may take a few seconds to become reachable';
const SHARE_HINT_PUBLIC = 'Closing this window keeps the tunnel running';

function shareEl(id) { return document.getElementById(id); }

function showShare(id, on) {
    const el = shareEl(id);
    if (el) el.classList.toggle('hidden', !on);
}

// Drives panes, footer buttons, hint, and the header globe from a status
// object {state, url, error}.
function renderShare(st) {
    if (!st || !st.state) return;
    const pub = st.state === 'public';
    const publishing = st.state === 'publishing';

    showShare('statePrivate', !pub && !publishing);
    showShare('statePublishing', publishing);
    showShare('statePublic', pub);
    showShare('goPublicBtn', !pub && !publishing);
    showShare('goPrivateBtn', pub);

    const status = shareEl('shareStatus');
    status.classList.toggle('is-public', pub);
    status.querySelector('.st').textContent = st.state.toUpperCase();

    const hint = shareEl('shareHint');
    if (st.state === 'error') {
        hint.textContent = st.error || 'Something went wrong';
        hint.classList.add('err');
    } else {
        hint.textContent = pub ? SHARE_HINT_PUBLIC : SHARE_HINT;
        hint.classList.remove('err');
    }

    shareEl('shareBtn').classList.toggle('share-on', pub);
    showShare('shareDot', pub);

    if (pub) {
        shareEl('connStr').textContent = st.url;
        drawQR(st.url);
    }
}

async function refreshShareStatus() {
    try {
        const res = await fetch('/api/share/status');
        if (res.ok) renderShare(await res.json());
    } catch (e) { /* globe keeps its last state; modal actions surface errors */ }
}

function openShareModal() {
    shareEl('shareModal').showModal();
    refreshShareStatus();
}

// POST a mutating action; both 200 and 502 carry the wrapper's status JSON.
async function shareAction(btn, path, optimistic) {
    if (btn.dataset.busy) return;
    btn.dataset.busy = '1';
    if (optimistic) renderShare({ state: optimistic });
    try {
        const res = await fetch(path, { method: 'POST' });
        renderShare(await res.json());
    } catch (e) {
        renderShare({ state: 'error', error: 'Request failed — is the holesail service running?' });
    } finally {
        delete btn.dataset.busy;
    }
}

function goPublic() {
    shareAction(shareEl('goPublicBtn'), '/api/share/start', 'publishing');
}

function goPrivate() {
    resetCopyLabel();
    shareAction(shareEl('goPrivateBtn'), '/api/share/stop');
}

function regenerateShareKey() {
    shareAction(shareEl('regenBtn'), '/api/share/regenerate');
}

function copyShareString() {
    const label = shareEl('copyBtn').querySelector('.lbl');
    const text = shareEl('connStr').textContent;
    const done = () => {
        label.textContent = 'COPIED ✓';
        setTimeout(resetCopyLabel, 1600);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done, done);
    } else {
        done();
    }
}

function resetCopyLabel() {
    const label = shareEl('copyBtn').querySelector('.lbl');
    if (label) label.textContent = 'COPY';
}

// Paints the connection string as a QR on the fixed-contrast canvas
// (ink-on-paper in both themes — ledger D16; scanners want dark-on-light).
function drawQR(text) {
    const canvas = shareEl('qrCanvas');
    if (!canvas || typeof qrcode !== 'function') return;
    const qr = qrcode(0, 'M');
    qr.addData(text);
    qr.make();
    const count = qr.getModuleCount();
    const quiet = 4; // quiet zone, in modules, each side
    const size = canvas.width;
    const scale = Math.max(1, Math.floor(size / (count + quiet * 2)));
    const offset = Math.floor((size - scale * count) / 2);
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = '#efe9dc';
    ctx.fillRect(0, 0, size, size);
    ctx.fillStyle = '#1a1714';
    for (let r = 0; r < count; r++) {
        for (let c = 0; c < count; c++) {
            if (qr.isDark(r, c)) ctx.fillRect(offset + c * scale, offset + r * scale, scale, scale);
        }
    }
}

// Reflect a live tunnel in the header after a reload/tab reopen.
refreshShareStatus();
