'use strict';

// Loads the real share.js ES module (via require()), installs the share modal's
// fake DOM, a scripted fetch, a recording qrcode stub, and a clipboard spy, then
// calls share.init() (which fetches status once, like the browser boot does).

const { FakeDocument, FakeElement } = require('./dom-stub');
const { makeTimers } = require('./timers');

const share = require('../share.js');

function loadShare({ fetchResponses = [] } = {}) {
    const document = new FakeDocument();

    // Sharing-panel elements share.js addresses by id.
    const ids = ['statePrivate', 'statePublishing', 'statePublic', 'connStr',
        'regenBtn', 'goPublicBtn', 'goPrivateBtn', 'shareHint'];
    ids.forEach(id => document.register(id, new FakeElement('div')));

    const shareStatus = new FakeElement('div');
    const statusLabel = new FakeElement('b');
    statusLabel.classList.add('st');
    shareStatus.appendChild(statusLabel);
    document.register('shareStatus', shareStatus);

    const copyBtn = new FakeElement('button');
    const copyLabel = new FakeElement('span');
    copyLabel.classList.add('lbl');
    copyLabel.textContent = 'COPY';
    copyBtn.appendChild(copyLabel);
    document.register('copyBtn', copyBtn);

    const qrCanvas = new FakeElement('canvas');
    qrCanvas.width = 232;
    qrCanvas.height = 232;
    const ctxCalls = [];
    qrCanvas.getContext = () => ({
        fillStyle: '',
        fillRect: (...args) => ctxCalls.push(args),
    });
    document.register('qrCanvas', qrCanvas);

    // Scripted fetch: shift the next response; record every call.
    const fetchCalls = [];
    const fetchImpl = (url, opts) => {
        fetchCalls.push({ url, method: (opts && opts.method) || 'GET' });
        const next = fetchResponses.shift() ||
            { ok: true, status: 200, body: { state: 'private', url: null, error: null } };
        return Promise.resolve({
            ok: next.ok !== false,
            status: next.status || 200,
            json: async () => next.body,
        });
    };

    const qrCalls = [];
    const qrcodeStub = () => ({
        addData: (t) => qrCalls.push(t),
        make() {},
        getModuleCount: () => 25,
        isDark: () => false,
    });

    const clipboardWrites = [];
    const timers = makeTimers();

    globalThis.document = document;
    globalThis.setTimeout = timers.setTimeout;
    globalThis.clearTimeout = timers.clearTimeout;
    globalThis.fetch = fetchImpl;
    globalThis.qrcode = qrcodeStub;
    // navigator is a read-only accessor on globalThis, so redefine it.
    Object.defineProperty(globalThis, 'navigator', {
        value: {
            clipboard: {
                writeText: (t) => { clipboardWrites.push(t); return Promise.resolve(); },
            },
        },
        configurable: true,
        writable: true,
    });

    share.init();

    // sandbox === globalThis so module global reads (fetch, qrcode, navigator)
    // see the stubs; expose share's exports as well.
    const sandbox = globalThis;
    for (const k of Object.keys(share)) {
        if (k !== 'init' && k !== 'default') sandbox[k] = share[k];
    }

    // Let queued promise callbacks (fetch chains) settle.
    async function settle() {
        for (let i = 0; i < 10; i++) await Promise.resolve();
    }

    return { document, sandbox, fetchCalls, qrCalls, clipboardWrites, flushTimers: timers.flush, settle };
}

module.exports = { loadShare };
