// Loads the real ES-module view sources, installs fake browser
// globals, and calls each module's init() so listeners wire onto a fresh
// FakeDocument and module state resets. Tests reach exports through env.sandbox
// (which is globalThis, so global reads like fetch/openSession see overrides).

import { FakeDocument, FakeElement } from './dom-stub.mjs';
import { makeTimers } from './timers.mjs';

import * as uiUtils from '../ui-utils.js';
import * as terminal from '../terminal.js';
import * as actions from '../actions.js';
import * as store from '../store.js';
import * as sidebar from '../sidebar.js';
import * as tabs from '../tabs.js';
import * as mobileBar from '../mobile-bar.js';
import * as picker from '../picker.js';
import * as historyDel from '../history-del.js';
import * as rename from '../rename.js';
import * as appInit from '../app-init.js';

// Namespaces whose exports the tests reach for via env.sandbox.
const NAMESPACES = [uiUtils, actions, store, sidebar, tabs, mobileBar, picker, historyDel, rename, appInit];
// history-del has no init; terminal.init wires browser-only listeners the view
// tests don't exercise. TerminalManager is stubbed instead. The store inits
// first (main.js order): the views subscribe to it in their inits.
const INIT_ORDER = [store, sidebar, tabs, mobileBar, picker, rename, appInit];

// Load views into a fresh fake environment with controllable globals/timers.
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
        // Mirror layout.html's injection of the shared route patterns.
        ROUTES: {
            sessions: '/api/sessions',
            settings: '/api/settings',
            uiPrefs: '/api/ui-prefs',
            directories: '/api/directories',
            sessionsHistory: '/api/sessions/history',
            sessionName: '/api/sessions/{terminalId}/name',
            sessionUpload: '/api/sessions/{terminalId}/upload',
            historyItem: '/api/sessions/history/{uuid}',
            wsTerminal: '/ws/terminal/{terminalId}',
        },
    };

    globalThis.window = window;
    globalThis.document = document;
    globalThis.WebSocket = { OPEN: 1 };
    globalThis.setTimeout = timers.setTimeout;
    globalThis.clearTimeout = timers.clearTimeout;
    globalThis.setInterval = () => ++intervalId;
    globalThis.clearInterval = () => {};
    globalThis.requestAnimationFrame = (fn) => { fn(); return 1; };
    globalThis.getComputedStyle = () => ({ getPropertyValue: () => (mobile ? '1' : '0') });
    globalThis.localStorage = {
        getItem: (k) => (k in localStorage ? localStorage[k] : null),
        setItem: (k, v) => { localStorage[k] = String(v); },
        removeItem: (k) => { delete localStorage[k]; },
    };
    globalThis.location = { reload() {} };
    globalThis.fetch = () => Promise.resolve({ ok: true, status: 200, json: async () => [] });

    // Stub TerminalManager so tests never hit the real xterm create path.
    terminal.TerminalManager.instances = {};
    Object.assign(terminal.TerminalManager, {
        create() {}, destroy() {}, resize() {}, resizeAll() {}, get() { return null; },
    });

    // Reset module state + wire listeners onto the fresh document.
    INIT_ORDER.forEach(m => m.init());

    // sandbox === globalThis so module global reads (fetch, openSession) see test
    // overrides; copy the exports the tests call onto it as well.
    const sandbox = globalThis;
    NAMESPACES.forEach(ns => {
        for (const k of Object.keys(ns)) {
            if (k !== 'init' && k !== 'default') sandbox[k] = ns[k];
        }
    });
    sandbox.TerminalManager = terminal.TerminalManager;

    return { document, window, sandbox, flushTimers: timers.flush, pendingTimers: timers.pending };
}

export { loadViews, FakeElement };
