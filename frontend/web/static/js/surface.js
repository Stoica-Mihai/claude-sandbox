// Client-side surface router. Dashboard and Logs share one shell — both are
// mounted in the DOM at once — so switching between them toggles visibility
// instead of navigating. The live session's WebSocket lives in
// #surface-dashboard and is never unmounted, so a Dashboard↔Logs switch causes
// no reconnect and no full-page reload stutter. history.pushState keeps the URL
// honest; popstate + direct URL loads still land on the right surface (the
// server renders the correct one visible). If the logs surface is absent (log
// sharing off over the tunnel), the nav falls through to normal navigation.

import { LogsManager } from './logs.js';
import { collapseSidebar } from './sidebar.js';

let current = 'dashboard';

const el = (id) => document.getElementById(id);

function setNavCurrent(surface) {
    document.querySelectorAll('.side-foot a.navitem').forEach((a) => {
        const href = a.getAttribute('href');
        const isCur = (href === '/logs' && surface === 'logs') || (href === '/' && surface === 'dashboard');
        if (isCur) a.setAttribute('aria-current', 'page');
        else a.removeAttribute('aria-current');
    });
}

export function switchTo(surface) {
    const logs = el('surface-logs');
    if (surface === 'logs' && !logs) return; // no logs surface here
    const toLogs = surface === 'logs';
    const dash = el('surface-dashboard');
    const main = document.querySelector('.main');
    const sub = el('surfaceSub');
    const kick = el('sideKick');
    const btnNew = document.querySelector('.side .btn-new');
    const sessionList = el('session-list');
    const logsSide = el('logs-side');

    if (dash) dash.classList.toggle('hidden', toLogs);
    if (logs) logs.classList.toggle('hidden', !toLogs);
    if (main) main.classList.toggle('main--logs', toLogs);
    if (sub) sub.textContent = toLogs ? 'logs' : 'dashboard';
    if (kick) kick.textContent = toLogs ? 'Logs' : 'Sessions';
    if (btnNew) btnNew.classList.toggle('hidden', toLogs);
    if (sessionList) sessionList.classList.toggle('hidden', toLogs);
    if (logsSide) logsSide.classList.toggle('hidden', !toLogs);

    if (toLogs) {
        if (!LogsManager.get()) LogsManager.create(logs);
    } else {
        LogsManager.destroy(); // stop the log/status SSE while on the dashboard
        // The session surface was display:none, so xterm/chat may have measured
        // at zero size; nudge a refit now that it is visible.
        window.dispatchEvent(new Event('resize'));
    }
    setNavCurrent(surface);
    current = surface;
}

export function init() {
    current = 'dashboard';
    if (typeof document === 'undefined') return;
    const logs = el('surface-logs');
    // Initial surface = whichever the server rendered visible.
    if (logs && !logs.classList.contains('hidden')) {
        current = 'logs';
        if (!LogsManager.get()) LogsManager.create(logs);
    }

    document.querySelectorAll('.side-foot a.navitem').forEach((a) => {
        a.addEventListener('click', (e) => {
            const href = a.getAttribute('href');
            if (href !== '/' && href !== '/logs') return;
            const target = href === '/logs' ? 'logs' : 'dashboard';
            if (target === 'logs' && !el('surface-logs')) return; // fall through to real nav
            e.preventDefault();
            if (target !== current) {
                switchTo(target);
                history.pushState({ surface: target }, '', href);
            }
            collapseSidebar(); // mobile: close the drawer after picking
        });
    });

    window.addEventListener('popstate', () => {
        switchTo(window.location.pathname === '/logs' ? 'logs' : 'dashboard');
    });
}
