// Leaf helpers with no cross-concern state.

// Mobile state is owned by CSS via the --is-mobile flag (which flips at the
// breakpoint); reading it here keeps the breakpoint defined in one place: app.css.

// ===== Mobile sidebar drawer =====
function isMobile() {
    return getComputedStyle(document.documentElement).getPropertyValue('--is-mobile').trim() === '1';
}

// Utility: escape HTML for safe insertion
function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

// Set a .btn's visible label without dropping its <span> (the kit counter-skews
// the span to keep the text upright; writing textContent would delete it).
function setBtnLabel(btn, label) {
    const span = btn.querySelector('span');
    if (span) span.textContent = label; else btn.textContent = label;
}

// Skeleton placeholder rows (kit .skel) shown while a fetch is in flight.
function dpSkelRows(count, height) {
    let html = '';
    for (let i = 0; i < count; i++) {
        html += `<div class="skel" style="height:${height}px;margin:8px 12px"></div>`;
    }
    return html;
}

// Wrap a row-action handler so the click doesn't bubble to the row/card select
// and the button's default action is suppressed.
function stopAnd(fn) {
    return (e) => { e.stopPropagation(); e.preventDefault(); fn(); };
}

// Base inline style for an .arow action button (history rows add a border-bottom).
const AROW_CSS = 'width:100%;background:var(--row-bg,transparent);border:none;text-align:left;font-family:inherit;color:inherit';

function relTime(unix) {
    const s = Math.floor(Date.now() / 1000) - unix;
    if (s < 60) return s + 's ago';
    if (s < 3600) return Math.floor(s / 60) + 'm ago';
    if (s < 86400) return Math.floor(s / 3600) + 'h ago';
    return Math.floor(s / 86400) + 'd ago';
}

// Format an elapsed-seconds count for a session card's duration: "2h 15m" / "45s".
// The client owns this format — the server no longer renders it.
function fmtDuration(sec) {
    let s = sec;
    const h = Math.floor(s / 3600); s %= 3600;
    const m = Math.floor(s / 60); s %= 60;
    if (h > 0) return m > 0 ? h + 'h ' + m + 'm' : h + 'h';
    if (m > 0) return s > 0 ? m + 'm ' + s + 's' : m + 'm';
    return s + 's';
}

// Kit toast (CSS lives in futurism.css; the .toaster host is built on demand).
// For notices that must outlive the modal, e.g. "created, git init failed".
function dpToast(msg) {
    let host = document.querySelector('.toaster');
    if (!host) {
        host = document.createElement('div');
        host.className = 'toaster';
        document.body.appendChild(host);
    }
    const el = document.createElement('div');
    el.className = 'toast err';
    el.setAttribute('role', 'status');
    el.textContent = msg;
    host.appendChild(el);
    setTimeout(() => el.classList.add('out'), 3600);
    setTimeout(() => el.remove(), 4000);
}
