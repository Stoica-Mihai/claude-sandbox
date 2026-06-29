'use strict';

const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { FakeDocument, FakeElement } = require('./dom-stub');

const VIEWS_PATH = path.join(__dirname, '..', 'views.js');

// Load views.js into a fresh sandbox with controllable globals/timers.
// Returns the live document plus a flushTimers() to fire pending setTimeout cbs.
function loadViews({ mobile = false, ids = [], localStorage = {} } = {}) {
    const document = new FakeDocument();
    ids.forEach(id => document.register(id, new FakeElement('div')));

    const pendingTimers = [];
    let intervalId = 0;

    const window = {
        matchMedia: () => ({ matches: mobile }),
        WebSocket: { OPEN: 1 },
        htmx: null,
    };

    const sandbox = {
        window,
        document,
        WebSocket: { OPEN: 1 },
        TextEncoder,
        Uint8Array,
        console: { log() {}, error() {}, warn() {} },
        setTimeout: (fn, ms) => { pendingTimers.push({ fn, ms }); return pendingTimers.length; },
        clearTimeout: () => {},
        setInterval: () => { return ++intervalId; },
        clearInterval: () => {},
        requestAnimationFrame: (fn) => { fn(); return 1; },
        getComputedStyle: () => ({ getPropertyValue: () => (mobile ? '1' : '0') }),
        localStorage: {
            getItem: (k) => (k in localStorage ? localStorage[k] : null),
            setItem: (k, v) => { localStorage[k] = String(v); },
            removeItem: (k) => { delete localStorage[k]; },
        },
        Date,
        Math,
        parseInt,
        parseFloat,
        location: { reload() {} },
        fetch: () => Promise.resolve({ ok: true, status: 200, json: async () => [] }),
        TerminalManager: {
            create() {}, destroy() {}, resize() {}, resizeAll() {},
            get() { return null; },
        },
    };
    sandbox.globalThis = sandbox;

    const code = fs.readFileSync(VIEWS_PATH, 'utf8');
    vm.createContext(sandbox);
    vm.runInContext(code, sandbox, { filename: 'views.js' });

    // Fire DOMContentLoaded so init paths register (harmless for these tests).
    document.dispatch('DOMContentLoaded', {});

    function flushTimers() {
        const due = pendingTimers.splice(0);
        due.forEach(t => t.fn());
    }

    return { document, window, sandbox, flushTimers, pendingTimers };
}

module.exports = { loadViews, FakeElement };
