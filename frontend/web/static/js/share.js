// Share tunnel UI: lives in the Sharing settings panel, driven by /api/share/*.
// No SSE — status is fetched when the panel opens and after each action (the
// wrapper answers mutations with the final state). When public, the whole app
// carries a `sharing-public` class so the logo mark glows (the ambient "you're
// exposed" signal, in place of a dedicated header glyph).

import { register } from './actions.js';
import { copyToClipboard } from './ui-utils.js';

const SHARE_HINT = 'Tunnel may take a few seconds to become reachable';
const SHARE_HINT_PUBLIC = 'The tunnel stays up until you go private';

// Share-tunnel state vocabulary, injected by layout.html from shared/types.go
// (window.SHARE_STATE) so this consumer branches on the same values the holesail
// producer emits, without re-typing them. Read lazily so tests that install the
// global after import still see it.
function shareStates() {
    const s = (typeof window !== 'undefined' && window.SHARE_STATE) || {};
    return { PRIVATE: s.private, PUBLISHING: s.publishing, PUBLIC: s.public, ERROR: s.error };
}

function shareEl(id) { return document.getElementById(id); }

function showShare(id, on) {
    const el = shareEl(id);
    if (el) el.classList.toggle('hidden', !on);
}

// Drives the sharing panel, its action buttons, and the ambient logo glow from
// a status object {state, url, error}.
export function renderShare(st) {
    if (!st || !st.state) return;
    const S = shareStates();
    const pub = st.state === S.PUBLIC;
    const publishing = st.state === S.PUBLISHING;
    const priv = !pub && !publishing;

    showShare('statePrivate', priv);
    showShare('statePublishing', publishing);
    showShare('statePublic', pub);
    showShare('goPublicBtn', priv);
    showShare('goPrivateBtn', pub);

    const status = shareEl('shareStatus');
    if (status) {
        status.classList.toggle('is-public', pub);
        status.querySelector('.st').textContent = st.state.toUpperCase();
    }

    const hint = shareEl('shareHint');
    if (hint) {
        if (st.state === S.ERROR) {
            hint.textContent = st.error || 'Something went wrong';
            hint.classList.add('err');
        } else {
            hint.textContent = pub ? SHARE_HINT_PUBLIC : SHARE_HINT;
            hint.classList.remove('err');
        }
    }

    document.body.classList.toggle('sharing-public', pub);

    if (pub) {
        shareEl('connStr').textContent = st.url;
        drawQR(st.url);
        // Going public resets the flag server-side; reflect the real value.
        refreshShareLogs();
    }
}

// --- Log sharing over the tunnel (frontend-native flag, off by default) ---

function setShareLogsToggle(on) {
    const btn = shareEl('shareLogsToggle');
    if (!btn) return;
    btn.classList.toggle('on', !!on);
    btn.setAttribute('aria-checked', on ? 'true' : 'false');
}

export async function refreshShareLogs() {
    try {
        const res = await fetch('/api/share/logs');
        if (res.ok) setShareLogsToggle(!!(await res.json()).enabled);
    } catch (e) { /* leave the toggle as-is; the guard still enforces the truth */ }
}

export async function toggleShareLogs(btn) {
    if (btn.dataset.busy) return;
    btn.dataset.busy = '1';
    const enabled = !btn.classList.contains('on');
    setShareLogsToggle(enabled); // optimistic
    try {
        const res = await fetch('/api/share/logs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled })
        });
        setShareLogsToggle(res.ok ? !!(await res.json()).enabled : !enabled);
    } catch (e) {
        setShareLogsToggle(!enabled); // revert
    } finally {
        delete btn.dataset.busy;
    }
}

export async function refreshShareStatus() {
    try {
        const res = await fetch('/api/share/status');
        if (res.ok) renderShare(await res.json());
    } catch (e) { /* the glow keeps its last state; panel actions surface errors */ }
}

// POST a mutating action; both 200 and 502 carry the wrapper's status JSON.
export async function shareAction(btn, path, optimistic) {
    if (btn.dataset.busy) return;
    btn.dataset.busy = '1';
    if (optimistic) renderShare({ state: optimistic });
    try {
        const res = await fetch(path, { method: 'POST' });
        renderShare(await res.json());
    } catch (e) {
        renderShare({ state: shareStates().ERROR, error: 'Request failed — is the holesail service running?' });
    } finally {
        delete btn.dataset.busy;
    }
}

export function goPublic() {
    shareAction(shareEl('goPublicBtn'), '/api/share/start', shareStates().PUBLISHING);
}

export function goPrivate() {
    resetCopyLabel();
    shareAction(shareEl('goPrivateBtn'), '/api/share/stop');
}

export function regenerateShareKey() {
    shareAction(shareEl('regenBtn'), '/api/share/regenerate');
}

export function copyShareString() {
    const btn = shareEl('copyBtn');
    if (!btn) return;
    const label = btn.querySelector('.lbl');
    const text = shareEl('connStr').textContent;
    copyToClipboard(text).then((ok) => {
        if (ok) {
            label.textContent = 'COPIED ✓';
            setTimeout(resetCopyLabel, 1600);
        } else {
            // Neither path copied (rare, locked-down browser): select the visible
            // string so the user can copy it by hand. Never claim success.
            selectConnString();
            label.textContent = 'COPY MANUALLY';
            setTimeout(resetCopyLabel, 2600);
        }
    });
}

// Select the visible connection string so the user can copy it manually when
// both programmatic paths fail. No-ops where Selection/Range are unavailable.
function selectConnString() {
    const el = shareEl('connStr');
    if (!el || typeof document.createRange !== 'function' ||
        typeof window === 'undefined' || !window.getSelection) return;
    const range = document.createRange();
    range.selectNodeContents(el);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
}

export function resetCopyLabel() {
    const label = shareEl('copyBtn').querySelector('.lbl');
    if (label) label.textContent = 'COPY';
}

// Paints the connection string as a QR on the fixed-contrast canvas
// (ink-on-paper in both themes — ledger D16; scanners want dark-on-light).
export function drawQR(text) {
    const canvas = shareEl('qrCanvas');
    if (!canvas || typeof qrcode !== 'function') return;
    if (canvas.dataset.drawn === text) return; // same string — canvas already correct
    canvas.dataset.drawn = text;
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

export function init() {
    register('share-copy', () => copyShareString());
    register('share-regen', () => regenerateShareKey());
    register('share-go-public', () => goPublic());
    register('share-go-private', () => goPrivate());
    register('share-logs-toggle', (el) => toggleShareLogs(el));

    // Reflect a live tunnel in the header after a reload/tab reopen.
    refreshShareStatus();
}
