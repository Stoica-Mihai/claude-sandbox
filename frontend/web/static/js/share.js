// Share tunnel UI: lives in the Sharing settings panel, driven by /api/share/*.
// No SSE — status is fetched when the panel opens and after each action (the
// wrapper answers mutations with the final state). When public, the whole app
// carries a `sharing-public` class so the logo mark glows (the ambient "you're
// exposed" signal, in place of a dedicated header glyph).

import { register } from './actions.js';

const SHARE_HINT = 'Tunnel may take a few seconds to become reachable';
const SHARE_HINT_PUBLIC = 'The tunnel stays up until you go private';

function shareEl(id) { return document.getElementById(id); }

function showShare(id, on) {
    const el = shareEl(id);
    if (el) el.classList.toggle('hidden', !on);
}

// Drives the sharing panel, its action buttons, and the ambient logo glow from
// a status object {state, url, error}.
export function renderShare(st) {
    if (!st || !st.state) return;
    const pub = st.state === 'public';
    const publishing = st.state === 'publishing';
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
        if (st.state === 'error') {
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
        renderShare({ state: 'error', error: 'Request failed — is the holesail service running?' });
    } finally {
        delete btn.dataset.busy;
    }
}

export function goPublic() {
    shareAction(shareEl('goPublicBtn'), '/api/share/start', 'publishing');
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

// Copy text, resolving true only on a real copy. Prefers the async Clipboard
// API (secure contexts), falling back to execCommand — the connection string
// grants full access, and the dashboard often runs over plain HTTP / the tunnel
// where the Clipboard API is unavailable, so a silent false success is worse
// than a visible failure.
function copyToClipboard(text) {
    if (navigator.clipboard?.writeText) {
        return navigator.clipboard.writeText(text).then(() => true, () => execCopy(text));
    }
    return Promise.resolve(execCopy(text));
}

// Legacy execCommand copy via a throwaway textarea; returns whether it copied.
function execCopy(text) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0';
    document.body.appendChild(ta);
    ta.select?.();
    let ok = false;
    try { ok = !!(document.execCommand && document.execCommand('copy')); } catch (e) { ok = false; }
    ta.remove();
    return ok;
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

    // Reflect a live tunnel in the header after a reload/tab reopen.
    refreshShareStatus();
}
