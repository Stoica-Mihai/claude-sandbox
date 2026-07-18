'use strict';

const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { FakeDocument, FakeElement } = require('./dom-stub');
const { makeTimers } = require('./timers');

const SHARE_PATH = path.join(__dirname, '..', 'share.js');

// Load share.js into a fresh sandbox with the share modal's DOM registered,
// a scripted fetch, a recording qrcode stub, and a clipboard spy.
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

    const sandbox = {
        document,
        console: { log() {}, error() {}, warn() {} },
        setTimeout: timers.setTimeout,
        clearTimeout: timers.clearTimeout,
        fetch: fetchImpl,
        qrcode: qrcodeStub,
        navigator: {
            clipboard: {
                writeText: (t) => { clipboardWrites.push(t); return Promise.resolve(); },
            },
        },
        Math,
    };
    sandbox.globalThis = sandbox;

    const code = fs.readFileSync(SHARE_PATH, 'utf8');
    vm.createContext(sandbox);
    vm.runInContext(code, sandbox, { filename: 'share.js' });

    // Let queued promise callbacks (fetch chains) settle.
    async function settle() {
        for (let i = 0; i < 10; i++) await Promise.resolve();
    }

    return { document, sandbox, fetchCalls, qrCalls, clipboardWrites, flushTimers: timers.flush, settle };
}

module.exports = { loadShare };
