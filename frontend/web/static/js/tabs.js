// Single-terminal tab view: tab lifecycle, tab bar, session-card wiring.

import { isMobile, escapeHtml, fmtDuration } from './ui-utils.js';
import { collapseSidebar } from './sidebar.js';
import { TerminalManager } from './terminal.js';
import { register } from './actions.js';
import { getSession, subscribe } from './store.js';

// Single terminal view with tabs.
export let singleTerminalId = null;    // currently active tab
export let singleTabs = [];            // array of open tab terminal IDs
// Last-known display name per tab, so a tab whose session ended keeps its
// label instead of degrading to a truncated dtach id.
const tabNames = {};

// Open a session terminal
export function openSession(terminalId) {
    if (!terminalId) return;
    if (isMobile()) collapseSidebar();
    openSessionSingle(terminalId);
    updateSessionCardStates();
}

export function openSessionSingle(terminalId) {
    const wrapper = document.getElementById('singleTerminal');
    if (!wrapper) return;

    // If already open as a tab, just switch to it
    if (singleTabs.includes(terminalId)) {
        switchSingleTab(terminalId);
        return;
    }

    // Create a container div for this tab's terminal
    const tabContainer = document.createElement('div');
    tabContainer.id = 'singleTab-' + terminalId;
    tabContainer.className = 'term-tab hidden';
    tabContainer.classList.add('terminal-bg');
    tabContainer.setAttribute('role', 'tabpanel');
    tabContainer.setAttribute('aria-labelledby', 'ttab-' + terminalId);
    wrapper.appendChild(tabContainer);

    // Add to tabs array
    singleTabs.push(terminalId);

    // Create the terminal in this tab's container
    TerminalManager.create(terminalId, tabContainer);

    // Switch to the new tab
    switchSingleTab(terminalId);
}

export function switchSingleTab(terminalId) {
    const wrapper = document.getElementById('singleTerminal');
    if (!wrapper) return;

    // Hide all tab containers, show the selected one
    singleTabs.forEach(id => {
        const el = document.getElementById('singleTab-' + id);
        if (el) el.classList.toggle('hidden', id !== terminalId);
    });

    singleTerminalId = terminalId;

    // Update tab bar, status bar, and sidebar card highlights
    updateSingleTabBar(terminalId);

    updateSessionCardStates();

    // Focus and resize
    requestAnimationFrame(() => {
        const instance = TerminalManager.get(terminalId);
        if (instance) {
            instance.term.focus();
            TerminalManager.resize(terminalId);
        }
    });
}

export function closeSingleTab(terminalId) {
    // Destroy the terminal
    TerminalManager.destroy(terminalId);

    // Remove the tab container
    const tabContainer = document.getElementById('singleTab-' + terminalId);
    if (tabContainer) tabContainer.remove();

    // Remove from tabs array
    singleTabs = singleTabs.filter(id => id !== terminalId);
    delete tabNames[terminalId];

    // If this was the active tab, switch to the last remaining tab or show welcome
    if (singleTerminalId === terminalId) {
        if (singleTabs.length > 0) {
            switchSingleTab(singleTabs[singleTabs.length - 1]);
        } else {
            singleTerminalId = null;
            updateSingleTabBar(null);

        }
    } else {
        // Just update the tab bar to remove the closed tab
        updateSingleTabBar(singleTerminalId);
    }
    updateSessionCardStates();
}

export function updateSingleWelcome(hasTerminal) {
    const welcome = document.getElementById('singleWelcome');
    const terminal = document.getElementById('singleTerminal');
    const controls = document.getElementById('singleControls');
    const mobileInput = document.getElementById('mobileInputBar');
    if (welcome) welcome.classList.toggle('hidden', hasTerminal);
    if (terminal) terminal.classList.toggle('hidden', !hasTerminal);
    if (controls) {
        // Only show controls bar on desktop (mobile has the input bar)
        if (isMobile()) {
            controls.classList.add('hidden');
        } else {
            controls.classList.toggle('hidden', !hasTerminal);
        }
    }
    if (mobileInput) mobileInput.classList.toggle('hidden', !hasTerminal);
}

export function updateSingleTabBar(activeTerminalId) {
    const tabBar = document.getElementById('singleTabBar');
    if (!tabBar) return;

    if (singleTabs.length === 0) {
        tabBar.innerHTML = '';
        updateSingleWelcome(false);
        return;
    }

    updateSingleWelcome(true);

    tabBar.innerHTML = singleTabs.map(id => {
        const session = getSession(id);
        if (session) tabNames[id] = session.display_name;
        const sessionName = session ? session.display_name : (tabNames[id] || id.substring(0, 8));
        const dead = !session; // gone from the server's session list = ended
        const isActive = id === activeTerminalId;
        const safeId = escapeHtml(id);
        return `
            <div class="ttab${isActive ? ' on' : ''}" id="ttab-${safeId}" data-terminal-id="${safeId}"
                 ${dead ? 'title="Session ended"' : ''}
                 role="tab" aria-selected="${isActive}" aria-controls="singleTab-${safeId}" tabindex="${isActive ? '0' : '-1'}"
                 data-action="switch-tab">
                <span class="tdot${dead ? ' dead' : isActive ? ' on' : ''}"></span>
                <span style="font-family:var(--mono);font-style:normal">${escapeHtml(sessionName)}</span>
                <button type="button" class="x" tabindex="-1" aria-label="Close tab" data-action="close-tab" data-terminal-id="${safeId}">&times;</button>
            </div>`;
    }).join('');
}

export function updateSessionCardStates() {
    document.querySelectorAll('.session-card').forEach(card => {
        const tid = card.dataset.terminalId;
        const active = singleTabs.includes(tid) && tid === singleTerminalId;
        card.classList.toggle('active', active);
        // aria-current mirrors the kit's .nav-v active semantics for screen readers.
        if (active) card.setAttribute('aria-current', 'page');
        else card.removeAttribute('aria-current');
    });
}

// Clean up terminal tab/view when a session is killed from the sidebar
export function cleanupKilledSession(terminalId) {
    // Close from single view tabs
    if (singleTabs.includes(terminalId)) {
        closeSingleTab(terminalId);
    }
}

// Refresh every session card's live duration from its data-created stamp.
export function tickDurations() {
    const now = Math.floor(Date.now() / 1000);
    document.querySelectorAll('.session-duration[data-created]').forEach(el => {
        const ts = parseInt(el.dataset.created, 10);
        if (ts) el.textContent = fmtDuration(now - ts);
    });
}

export function init() {
    singleTerminalId = null;
    singleTabs = [];
    for (const k in tabNames) delete tabNames[k];

    register('switch-tab', (el) => switchSingleTab(el.dataset.terminalId));
    register('close-tab', (el, e) => { e.stopPropagation(); closeSingleTab(el.dataset.terminalId); });
    register('kill-cleanup', (el) => cleanupKilledSession(el.dataset.terminalId));

    // Middle-click a tab to close it (delegated auxclick — no kit precedent).
    document.addEventListener('auxclick', (e) => {
        if (e.button !== 1) return;
        const tab = e.target.closest?.('.ttab[data-terminal-id]');
        if (!tab) return;
        e.preventDefault();
        closeSingleTab(tab.dataset.terminalId);
    });

    // Keyboard contract for the tab strip (role=tablist/tab, roving tabindex) —
    // mirrors the kit's .tabs/fdTab pattern (ArrowLeft/Right move+activate,
    // Enter/Space activate) by hand rather than adopting .tabs/fdInit, since this
    // app hand-wires its own interaction logic throughout rather than depending
    // on futurism.js. Delete/Backspace-to-close has no kit precedent (the kit's
    // plain tab has no close affordance) — an app-original extension.
    document.addEventListener('keydown', (e) => {
        const tab = e.target.closest && e.target.closest('.ttab[role="tab"]');
        if (!tab) return;
        const bar = document.getElementById('singleTabBar');
        if (!bar) return;
        const tabs = Array.from(bar.querySelectorAll('.ttab[role="tab"]'));
        const i = tabs.indexOf(tab);
        const id = tab.dataset.terminalId;

        if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
            e.preventDefault();
            const n = e.key === 'ArrowRight' ? (i + 1) % tabs.length : (i - 1 + tabs.length) % tabs.length;
            const nextId = tabs[n].dataset.terminalId;
            switchSingleTab(nextId);
            document.getElementById('ttab-' + nextId)?.focus();
        } else if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            switchSingleTab(id);
            document.getElementById('ttab-' + id)?.focus();
        } else if (e.key === 'Delete' || e.key === 'Backspace') {
            e.preventDefault();
            closeSingleTab(id);
            if (singleTerminalId) {
                document.getElementById('ttab-' + singleTerminalId)?.focus();
            } else {
                document.querySelector('.btn-new')?.focus();
            }
        }
    });

    // Session card click delegation — handles all clicks on session cards
    document.addEventListener('click', (e) => {
        const card = e.target.closest('.session-card');
        if (!card) return;

        // Do not intercept button clicks inside card
        if (e.target.closest('button')) return;

        const terminalId = card.dataset.terminalId;
        if (!terminalId) return;

        // Regular click: open in single view
        openSession(terminalId);
    });

    // Keyboard activation: the card is role="button" tabindex="0", so Enter/Space
    // open it — but only when the card itself is focused, not a nested action button.
    document.addEventListener('keydown', (e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        const card = e.target.closest && e.target.closest('.session-card');
        if (!card || card !== e.target) return;
        e.preventDefault();
        const terminalId = card.dataset.terminalId;
        if (terminalId) openSession(terminalId);
    });

    // Re-render on every session-list change (the store re-reads after each
    // HTMX sidebar swap): card active states, fresh cards' durations, and the
    // tab bar — renames propagate to tab labels, and tabs whose session ended
    // get their dead marker. Skip the tab bar when focus is inside the strip
    // so an SSE update can't yank keyboard focus.
    subscribe(() => {
        updateSessionCardStates();
        tickDurations();
        const bar = document.getElementById('singleTabBar');
        if (singleTabs.length && bar && !bar.contains(document.activeElement)) {
            updateSingleTabBar(singleTerminalId);
        }
    });
}
