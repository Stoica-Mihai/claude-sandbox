'use strict';

const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { FakeDocument, FakeElement } = require('./dom-stub');
const { makeTimers } = require('./timers');

const VIEWS_FILES = [
    'ui-utils.js',
    'sidebar.js',
    'tabs.js',
    'mobile-bar.js',
    'picker.js',
    'history-del.js',
    'rename.js',
    'app-init.js',
];

// Load views.js into a fresh sandbox with controllable globals/timers.
// Returns the live document plus a flushTimers() to fire pending setTimeout cbs.
function loadViews({ mobile = false, ids = [], localStorage = {} } = {}) {
    const document = new FakeDocument();
    ids.forEach(id => document.register(id, new FakeElement('div')));

    const timers = makeTimers();
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
        setTimeout: timers.setTimeout,
        clearTimeout: timers.clearTimeout,
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

    const code = VIEWS_FILES
        .map(f => fs.readFileSync(path.join(__dirname, '..', f), 'utf8'))
        .join('\n');
    vm.createContext(sandbox);
    vm.runInContext(code, sandbox, { filename: 'views.js' });

    // Fire DOMContentLoaded so init paths register (harmless for these tests).
    document.dispatch('DOMContentLoaded', {});

    return { document, window, sandbox, flushTimers: timers.flush, pendingTimers: timers.pending };
}

module.exports = { loadViews, FakeElement };
