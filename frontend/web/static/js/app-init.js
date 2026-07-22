// App init: keyboard shortcuts and load-time bootstrap.

import { applySidebar } from './sidebar.js';
import { isMobile } from './ui-utils.js';
import { openNewSessionModal } from './picker.js';
import { openSession, tickDurations } from './tabs.js';
import { getSessions, subscribe } from './store.js';

// ===== Keyboard shortcuts (desktop only) =====
export function handleShortcuts(e) {
    // Guard: do not fire when a modal dialog is open
    if (document.querySelector('dialog[open]')) return;

    // Guard: do not fire when focus is in an input/textarea (non-terminal)
    const tag = document.activeElement?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') {
        if (!document.activeElement.classList.contains('xterm-helper-textarea')) {
            return;
        }
    }

    const key = e.key;

    // Alt+N — Open new session modal
    if (e.altKey && key === 'n') {
        e.preventDefault();
        openNewSessionModal({});
        return;
    }

    // Alt+1 through Alt+9 — Show the Nth session from the sidebar list.
    if (e.altKey) {
        const digit = parseInt(key, 10);
        if (digit >= 1 && digit <= 9) {
            e.preventDefault();
            const sessions = getSessions();
            const session = sessions[digit - 1];
            if (session) openSession(session.name);
            return;
        }
    }
}

export function init() {
    applySidebar(localStorage.getItem('sidebar') === 'expanded');

    // Header session badge follows the store.
    subscribe((sessions) => {
        const badgeText = document.getElementById('session-badge-text');
        if (!badgeText) return;
        const count = sessions.length;
        badgeText.textContent = `${count} session${count !== 1 ? 's' : ''}`;
        badgeText.classList.toggle('alive', count > 0);
    });

    // Register keyboard shortcuts on desktop only
    if (!isMobile()) {
        document.addEventListener('keydown', handleShortcuts);
    }

    // Fill durations now, then tick every second (client owns the format).
    tickDurations();
    setInterval(tickDurations, 1000);
}
