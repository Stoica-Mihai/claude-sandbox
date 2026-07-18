// Delegated event dispatch: one document click listener resolves data-action.

const handlers = {};

// Record a handler for a data-action name. Handler receives (element, event).
export function register(name, handler) {
    handlers[name] = handler;
}

// Install one delegated click listener: on click, find the nearest
// [data-action] ancestor and invoke its registered handler.
export function initActions() {
    document.addEventListener('click', (e) => {
        const el = e.target.closest?.('[data-action]');
        if (!el) return;
        const fn = handlers[el.dataset.action];
        if (fn) fn(el, e);
    });
}
