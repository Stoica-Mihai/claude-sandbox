// App init: keyboard shortcuts, pull-to-refresh, and load-time bootstrap.

import { applySidebar } from './sidebar.js';
import { isMobile } from './ui-utils.js';
import { openNewSessionModal } from './picker.js';
import { closeSingleTab, switchSingleTab, singleTerminalId, singleTabs, tickDurations } from './tabs.js';

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

    // Alt+W — Close current tab
    if (e.altKey && key === 'w') {
        e.preventDefault();
        if (singleTerminalId) {
            closeSingleTab(singleTerminalId);
        }
        return;
    }

    // Alt+1 through Alt+9 — Switch to tab N
    if (e.altKey) {
        const digit = parseInt(key, 10);
        if (digit >= 1 && digit <= 9) {
            e.preventDefault();
            const index = digit - 1;
            if (index < singleTabs.length) {
                switchSingleTab(singleTabs[index]);
            }
            return;
        }
    }
}

// ===== Pull to refresh =====
export function initPullToRefresh() {
    const body = document.body;
    const indicator = document.getElementById('pullIndicator');
    if (!indicator) return;
    const label = indicator.querySelector('.pull-label');

    const threshold = 80;   // px pull to arm a refresh
    const maxPull = 100;    // px where the bar reaches full height
    const barHeight = 40;   // px bar height when fully revealed
    let startY = 0;
    let pulling = false;

    const reset = () => {
        indicator.style.height = '0';
        indicator.style.setProperty('--pull', '0');
        indicator.classList.remove('armed', 'refreshing');
    };

    body.addEventListener('touchstart', (e) => {
        // Don't activate inside terminals, open dialogs, or the sidebar drawer
        if (e.target.closest('.xterm') || e.target.closest('#singleTerminal')) return;
        if (document.querySelector('dialog[open]')) return;
        if (e.target.closest('#sidebar')) return;
        startY = e.touches[0].clientY;
        pulling = true;
    }, { passive: true });

    body.addEventListener('touchmove', (e) => {
        if (!pulling) return;
        const dy = e.touches[0].clientY - startY;
        if (dy < 0) { pulling = false; reset(); return; }
        const clamped = Math.min(dy, maxPull);
        indicator.style.height = (clamped / maxPull * barHeight) + 'px';
        indicator.style.setProperty('--pull', String(Math.min(dy / threshold, 1)));
        const armed = dy >= threshold;
        indicator.classList.toggle('armed', armed);
        if (label) label.textContent = armed ? 'Release to refresh' : 'Pull to refresh';
    }, { passive: true });

    body.addEventListener('touchend', () => {
        if (!pulling) return;
        pulling = false;
        if (indicator.classList.contains('armed')) {
            indicator.classList.add('refreshing');
            indicator.style.height = barHeight + 'px';
            if (label) label.textContent = 'Refreshing';
            setTimeout(() => location.reload(), 400);
        } else {
            reset();
        }
    });
}

export function init() {
    applySidebar(localStorage.getItem('sidebar') === 'expanded');
    initPullToRefresh();

    // Register keyboard shortcuts on desktop only
    if (!isMobile()) {
        document.addEventListener('keydown', handleShortcuts);
    }

    // Fill durations now, then tick every second (client owns the format).
    tickDurations();
    setInterval(tickDurations, 1000);
}
