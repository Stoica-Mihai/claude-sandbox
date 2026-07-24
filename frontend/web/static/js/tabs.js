// Single-terminal view driven by the sidebar. One session is shown at a time;
// selecting another sidebar card tears down the current terminal (the session
// keeps running server-side — sessiond owns it) and shows the selected one.
// There is no tab strip: the sidebar session list is the switcher.

import { isMobile, fmtDuration } from './ui-utils.js';
import { collapseSidebar } from './sidebar.js';
import { TerminalManager } from './terminal.js';
import { ChatManager, requestModeSwitch } from './chat.js';
import { register } from './actions.js';
import { subscribe, getSession } from './store.js';

// The session currently shown in the single view (either surface), or null
// (welcome screen).
export let singleTerminalId = null;

// Persist the shown session so navigating away (e.g. to /logs, a full page
// load) and back to the dashboard reopens it — the process + scrollback survive
// in sessiond, so reopening re-attaches and repaints from the snapshot.
const ACTIVE_KEY = 'activeSession';
let restored = false;

function saveActive(id) {
    try { localStorage.setItem(ACTIVE_KEY, id); } catch { /* storage disabled */ }
}
function clearActive() {
    try { localStorage.removeItem(ACTIVE_KEY); } catch { /* storage disabled */ }
}

// restoreActive reopens the persisted session on dashboard load, once, if it is
// still live. No-ops on /logs (no session surface) and once a session is shown.
// Retried from the store subscription until the list has loaded.
function restoreActive() {
    if (restored || singleTerminalId) return;
    if (!document.getElementById('singleWelcome')) return; // dashboard only
    let saved;
    try { saved = localStorage.getItem(ACTIVE_KEY); } catch { return; }
    if (saved && getSession(saved)) {
        restored = true;
        openSession(saved);
    }
}

// sessionKind resolves a session's kind from the client store, defaulting to
// terminal for a session the store doesn't (yet) know about.
function sessionKind(terminalId) {
    return getSession(terminalId)?.kind || 'terminal';
}

// kindOverride bypasses the store lookup when the caller already knows the
// kind — a mode switch opens the respawned session before the sidebar
// fragment (and thus the store) has caught up.
// Show a session in the single view (terminal or chat surface, by kind), or
// focus it if it is already shown.
export function openSession(terminalId, kindOverride) {
    if (!terminalId) return;
    if (isMobile()) collapseSidebar();

    if (terminalId === singleTerminalId && (TerminalManager.get(terminalId) || ChatManager.get(terminalId))) {
        focusActive();
        return;
    }

    // Stop showing the previous session. It keeps running on the server; a later
    // switch back re-attaches (terminal: repaints from sessiond's snapshot; chat:
    // re-fetches the transcript and resubscribes to the live tail).
    teardownCurrent();

    const kind = kindOverride || sessionKind(terminalId);
    singleTerminalId = terminalId;
    saveActive(terminalId);

    if (kind === 'chat') {
        const wrapper = document.getElementById('singleChat');
        if (!wrapper) return;
        ChatManager.create(terminalId, wrapper);
    } else {
        const wrapper = document.getElementById('singleTerminal');
        if (!wrapper) return;
        const container = document.createElement('div');
        container.id = 'singleTerm-' + terminalId;
        container.className = 'term-tab terminal-bg';
        container.setAttribute('role', 'tabpanel');
        wrapper.appendChild(container);
        TerminalManager.create(terminalId, container);
    }

    updateSingleWelcome(true, kind);
    updateSessionCardStates();
    focusActive();
}

// teardownCurrent disposes whichever surface is shown and removes its container.
function teardownCurrent() {
    if (!singleTerminalId) return;
    TerminalManager.destroy(singleTerminalId);
    ChatManager.destroy(singleTerminalId);
    document.getElementById('singleTerm-' + singleTerminalId)?.remove();
    singleTerminalId = null;
}

// focusActive focuses + resizes the shown surface after the next frame (needs
// a frame for the container to have dimensions).
function focusActive() {
    const id = singleTerminalId;
    requestAnimationFrame(() => {
        const instance = TerminalManager.get(id);
        if (instance) {
            instance.term.focus();
            TerminalManager.resize(id);
            return;
        }
        if (ChatManager.get(id)) ChatManager.focus(id);
    });
}

// updateSingleWelcome toggles the welcome screen vs the shown surface + its
// controls. Chat sessions get their own header/input bar, so the terminal-only
// desktop keycap bar and mobile input bar stay hidden for them.
export function updateSingleWelcome(hasSession, kind = 'terminal') {
    const welcome = document.getElementById('singleWelcome');
    const terminal = document.getElementById('singleTerminal');
    const chat = document.getElementById('singleChat');
    const controls = document.getElementById('singleControls');
    const mobileInput = document.getElementById('mobileInputBar');
    const isChat = hasSession && kind === 'chat';
    if (welcome) welcome.classList.toggle('hidden', hasSession);
    if (terminal) terminal.classList.toggle('hidden', !hasSession || isChat);
    if (chat) chat.classList.toggle('hidden', !isChat);
    if (controls) {
        // Desktop controls only; mobile has its own input bar. Neither applies
        // to the chat surface, which carries its own header and input bar.
        controls.classList.toggle('hidden', isMobile() || !hasSession || isChat);
    }
    if (mobileInput) mobileInput.classList.toggle('hidden', !hasSession || isChat);
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
    clearActive(); // back to welcome: don't reopen this session on return
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
    restored = false;

    register('kill-cleanup', (el) => cleanupKilledSession(el.dataset.terminalId));

    // Mode switch (both directions): kill the session named on the control
    // (or the currently shown one, for the static terminal-controls button)
    // and open whatever gets respawned in its place.
    register('mode-switch', async (el) => {
        const terminalId = el.dataset.terminalId || singleTerminalId;
        const targetKind = el.dataset.targetKind;
        if (!terminalId || !targetKind) return;
        const newName = await requestModeSwitch(terminalId, targetKind);
        if (newName) openSession(newName, targetKind);
    });

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
    // sidebar swap): active-card highlight and fresh cards' durations. Also retry
    // the one-shot restore until the session list has loaded.
    subscribe(() => {
        restoreActive();
        updateSessionCardStates();
        tickDurations();
    });

    // Reopen the last session AFTER the sync init sequence: TerminalManager /
    // ChatManager reset their instance registries in their own init()s, which
    // main.js runs after this one — a synchronous restore here would create the
    // instance and then have it wiped. queueMicrotask runs it once main.js's
    // init loop unwinds, with every manager ready.
    queueMicrotask(restoreActive);
}
