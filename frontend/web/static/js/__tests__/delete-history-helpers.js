'use strict';

// Shared setup helpers for the delete-session-history view tests
// (delete-session-history.test.js + views.delete-history.test.js).

// Click event that records whether it was stopped / default-prevented.
function clickEvent() {
    return {
        _stopped: false,
        _prevented: false,
        stopPropagation() { this._stopped = true; },
        preventDefault() { this._prevented = true; },
    };
}

// Fire a .row-act's idle trash button (its .row-act-btn child); returns the event.
function clickTrash(act, e = clickEvent()) {
    const btn = act.children.find(c => c.classList.contains('row-act-btn'));
    btn.onclick(e);
    return e;
}

module.exports = { clickEvent, clickTrash };
