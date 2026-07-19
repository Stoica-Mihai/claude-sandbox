// Single-terminal view driven by the sidebar. One session is shown at a time;
// selecting another sidebar card tears down the current terminal (the session
// keeps running server-side — sessiond owns it) and shows the selected one.
// There is no tab strip: the sidebar session list is the switcher.

import { isMobile, fmtDuration } from './ui-utils.js';
import { collapseSidebar } from './sidebar.js';
import { TerminalManager } from './terminal.js';
import { register } from './actions.js';
import { subscribe } from './store.js';

// The session currently shown in the terminal view, or null (welcome screen).
export let singleTerminalId = null;

// Show a session in the terminal view, or focus it if it is already shown.
export function openSession(terminalId) {
    if (!terminalId) return;
    if (isMobile()) collapseSidebar();

    if (terminalId === singleTerminalId && TerminalManager.get(terminalId)) {
        focusActive();
        return;
    }

    // Stop showing the previous session. It keeps running on the server; a later
    // switch back re-attaches and repaints from sessiond's snapshot.
    teardownCurrent();

    const wrapper = document.getElementById('singleTerminal');
    if (!wrapper) return;

    const container = document.createElement('div');
    container.id = 'singleTerm-' + terminalId;
    container.className = 'term-tab terminal-bg';
    container.setAttribute('role', 'tabpanel');
    wrapper.appendChild(container);

    singleTerminalId = terminalId;
    TerminalManager.create(terminalId, container);
    updateSingleWelcome(true);
    updateSessionCardStates();
    focusActive();
}

// teardownCurrent disposes the shown terminal and removes its container.
function teardownCurrent() {
    if (!singleTerminalId) return;
    TerminalManager.destroy(singleTerminalId);
    document.getElementById('singleTerm-' + singleTerminalId)?.remove();
    singleTerminalId = null;
}

// focusActive focuses + resizes the shown terminal after the next frame (needs
// a frame for the container to have dimensions).
function focusActive() {
    const id = singleTerminalId;
    requestAnimationFrame(() => {
        const instance = TerminalManager.get(id);
        if (instance) {
            instance.term.focus();
            TerminalManager.resize(id);
        }
    });
}

// updateSingleWelcome toggles the welcome screen vs the terminal + its controls.
export function updateSingleWelcome(hasTerminal) {
    const welcome = document.getElementById('singleWelcome');
    const terminal = document.getElementById('singleTerminal');
    const controls = document.getElementById('singleControls');
    const mobileInput = document.getElementById('mobileInputBar');
    if (welcome) welcome.classList.toggle('hidden', hasTerminal);
    if (terminal) terminal.classList.toggle('hidden', !hasTerminal);
    if (controls) {
        // Desktop controls only; mobile has its own input bar.
        controls.classList.toggle('hidden', isMobile() || !hasTerminal);
    }
    if (mobileInput) mobileInput.classList.toggle('hidden', !hasTerminal);
}

// updateSessionCardStates highlights the sidebar card of the shown session.
export function updateSessionCardStates() {
    document.querySelectorAll('.session-card').forEach(card => {
        const active = card.dataset.terminalId === singleTerminalId;
        card.classList.toggle('active', active);
        // aria-current mirrors the kit's .nav-v active semantics for screen readers.
        if (active) card.setAttribute('aria-current', 'page');
        else card.removeAttribute('aria-current');
    });
}

// cleanupKilledSession clears the view when the shown session is killed.
export function cleanupKilledSession(terminalId) {
    if (terminalId !== singleTerminalId) return;
    teardownCurrent();
    updateSingleWelcome(false);
    updateSessionCardStates();
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

    register('kill-cleanup', (el) => cleanupKilledSession(el.dataset.terminalId));

    // Session card click → show that session (ignore clicks on the card's buttons).
    document.addEventListener('click', (e) => {
        const card = e.target.closest('.session-card');
        if (!card || e.target.closest('button')) return;
        const terminalId = card.dataset.terminalId;
        if (terminalId) openSession(terminalId);
    });

    // Keyboard: the card is role="button" tabindex="0", so Enter/Space open it —
    // but only when the card itself is focused, not a nested action button.
    document.addEventListener('keydown', (e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        const card = e.target.closest && e.target.closest('.session-card');
        if (!card || card !== e.target) return;
        e.preventDefault();
        const terminalId = card.dataset.terminalId;
        if (terminalId) openSession(terminalId);
    });

    // Re-render on every session-list change (the store re-reads after each HTMX
    // sidebar swap): active-card highlight and fresh cards' durations.
    subscribe(() => {
        updateSessionCardStates();
        tickDurations();
    });
}
