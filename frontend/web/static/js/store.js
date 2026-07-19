// Client session store: single source of truth for the server session list.
// Fed by the JSON payload embedded in the sessions fragment; views subscribe
// and render from this state instead of scraping the sidebar DOM.

const listeners = new Set();
let sessions = [];

export function getSessions() { return sessions; }

// Find a session by its terminal id, or null if it ended.
export function getSession(terminalId) {
    return sessions.find(s => s.name === terminalId) || null;
}

// Subscribe to session-list changes; returns an unsubscribe function.
export function subscribe(fn) {
    listeners.add(fn);
    return () => listeners.delete(fn);
}

function setSessions(next) {
    sessions = next;
    listeners.forEach(fn => fn(sessions));
}

// Parse the #session-data JSON embedded in the sessions fragment.
export function readSessionsFromDOM() {
    const el = document.getElementById('session-data');
    if (!el) return;
    try {
        setSessions(JSON.parse(el.textContent) || []);
    } catch (e) { /* malformed payload — keep last known state */ }
}

export function init() {
    sessions = [];
    listeners.clear();

    // Every SSE-triggered sidebar swap carries a fresh payload.
    document.addEventListener('htmx:afterSwap', (e) => {
        if (e.target?.id === 'session-list') readSessionsFromDOM();
    });

    readSessionsFromDOM();
}
