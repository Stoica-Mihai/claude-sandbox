import test from 'node:test';
import assert from 'node:assert/strict';
import { loadShare } from './load-share.mjs';

const PRIVATE = { ok: true, body: { state: 'private', url: null, error: null } };
const PUBLIC = { ok: true, body: { state: 'public', url: 'hs://s000abc123', error: null } };

test('script load fetches status once and renders private', async () => {
    const env = loadShare({ fetchResponses: [PRIVATE] });
    await env.settle();

    assert.equal(env.fetchCalls.length, 1);
    assert.deepEqual(env.fetchCalls[0], { url: '/api/share/status', method: 'GET' });
    const doc = env.document;
    assert.equal(doc.getElementById('statePrivate').classList.contains('hidden'), false);
    assert.equal(doc.getElementById('statePublic').classList.contains('hidden'), true);
    assert.equal(doc.getElementById('goPublicBtn').classList.contains('hidden'), false);
    assert.equal(doc.body.classList.contains('sharing-public'), false);
});

test('page load with a live tunnel sets the ambient public glow', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC] });
    await env.settle();

    assert.equal(env.document.body.classList.contains('sharing-public'), true);
});

test('refreshShareStatus re-fetches on demand (panel open)', async () => {
    const env = loadShare({ fetchResponses: [PRIVATE, PRIVATE] });
    await env.settle();

    env.sandbox.refreshShareStatus();
    await env.settle();

    assert.equal(env.fetchCalls.length, 2);
});

test('goPublic posts start, shows publishing in flight, renders public result', async () => {
    const env = loadShare({ fetchResponses: [PRIVATE] });
    await env.settle();

    // Hold the start response until we've observed the optimistic state.
    let release;
    const held = new Promise(res => { release = res; });
    const realFetch = env.sandbox.fetch;
    env.sandbox.fetch = (url, opts) => {
        if (url === '/api/share/start') {
            env.fetchCalls.push({ url, method: opts.method });
            return held.then(() => ({ ok: true, status: 200, json: async () => PUBLIC.body }));
        }
        return realFetch(url, opts);
    };

    const p = env.sandbox.goPublic();
    const doc = env.document;
    assert.equal(doc.getElementById('statePublishing').classList.contains('hidden'), false);
    assert.equal(doc.getElementById('goPublicBtn').classList.contains('hidden'), true);
    assert.equal(doc.getElementById('shareStatus').querySelector('.st').textContent, 'PUBLISHING');

    release();
    await p;
    await env.settle();

    assert.equal(doc.getElementById('statePublic').classList.contains('hidden'), false);
    assert.equal(doc.getElementById('connStr').textContent, 'hs://s000abc123');
    assert.deepEqual(env.qrCalls, ['hs://s000abc123']);
    assert.equal(doc.body.classList.contains('sharing-public'), true);
    assert.equal(doc.getElementById('goPrivateBtn').classList.contains('hidden'), false);
});

test('goPrivate posts stop and returns to private', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC, PRIVATE] });
    await env.settle(); // load renders public

    env.sandbox.goPrivate();
    await env.settle();

    const doc = env.document;
    assert.deepEqual(env.fetchCalls[1], { url: '/api/share/stop', method: 'POST' });
    assert.equal(doc.getElementById('statePrivate').classList.contains('hidden'), false);
    assert.equal(doc.body.classList.contains('sharing-public'), false);
});

test('regenerate renders the new string and redraws the QR', async () => {
    const NEW = { ok: true, body: { state: 'public', url: 'hs://s000fresh', error: null } };
    const env = loadShare({ fetchResponses: [PUBLIC, NEW] });
    await env.settle();

    env.sandbox.regenerateShareKey();
    await env.settle();

    assert.deepEqual(env.fetchCalls[1], { url: '/api/share/regenerate', method: 'POST' });
    assert.equal(env.document.getElementById('connStr').textContent, 'hs://s000fresh');
    assert.deepEqual(env.qrCalls, ['hs://s000abc123', 'hs://s000fresh']);
});

test('start failure surfaces the wrapper error and re-enables GO PUBLIC', async () => {
    const FAIL = {
        ok: false, status: 502,
        body: { state: 'error', url: null, error: 'tunnel did not become ready in time' },
    };
    const env = loadShare({ fetchResponses: [PRIVATE, FAIL] });
    await env.settle();

    env.sandbox.goPublic();
    await env.settle();

    const doc = env.document;
    const hint = doc.getElementById('shareHint');
    assert.equal(hint.classList.contains('err'), true);
    assert.equal(hint.textContent, 'tunnel did not become ready in time');
    assert.equal(doc.getElementById('goPublicBtn').classList.contains('hidden'), false);
    assert.equal(doc.getElementById('goPublicBtn').dataset.busy, undefined);
});

test('copy writes the string to the clipboard and flashes the label', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC] });
    await env.settle();

    env.sandbox.copyShareString();
    await env.settle();

    assert.deepEqual(env.clipboardWrites, ['hs://s000abc123']);
    const label = env.document.getElementById('copyBtn').querySelector('.lbl');
    assert.equal(label.textContent, 'COPIED ✓');
    env.flushTimers();
    assert.equal(label.textContent, 'COPY');
});

test('copy falls back to execCommand when the Clipboard API rejects', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC] });
    await env.settle();

    // Clipboard API present but rejects (e.g. permission denied in a non-secure context).
    globalThis.navigator = { clipboard: { writeText: () => Promise.reject(new Error('denied')) } };
    let execArg = null;
    env.document.execCommand = (cmd) => { execArg = cmd; return true; };

    env.sandbox.copyShareString();
    await env.settle();

    assert.equal(execArg, 'copy', 'used the execCommand fallback');
    const label = env.document.getElementById('copyBtn').querySelector('.lbl');
    assert.equal(label.textContent, 'COPIED ✓', 'reports success when the fallback copied');
});

test('copy never fakes success: no Clipboard API and a failing execCommand', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC] });
    await env.settle();

    globalThis.navigator = {}; // no clipboard API (non-secure context)
    env.document.execCommand = () => false;

    env.sandbox.copyShareString();
    await env.settle();

    const label = env.document.getElementById('copyBtn').querySelector('.lbl');
    assert.notEqual(label.textContent, 'COPIED ✓', 'must not claim success when nothing copied');
    assert.equal(label.textContent, 'COPY MANUALLY');
});

test('going public reflects the server log-share flag on the toggle', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC], shareLogsEnabled: true });
    await env.settle();

    const btn = env.document.getElementById('shareLogsToggle');
    assert.equal(btn.classList.contains('on'), true);
    assert.equal(btn.getAttribute('aria-checked'), 'true');
    // The public render triggered exactly one GET to read the flag.
    assert.deepEqual(env.shareLogsCalls, [{ method: 'GET', enabled: true }]);
});

test('toggling share-logs POSTs the new value and flips the switch', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC] }); // starts off
    await env.settle();

    const btn = env.document.getElementById('shareLogsToggle');
    assert.equal(btn.classList.contains('on'), false);

    env.sandbox.toggleShareLogs(btn);
    await env.settle();

    assert.equal(btn.classList.contains('on'), true);
    assert.equal(btn.getAttribute('aria-checked'), 'true');
    const posts = env.shareLogsCalls.filter(c => c.method === 'POST');
    assert.deepEqual(posts, [{ method: 'POST', enabled: true }]);
});

test('busy guard: a second goPublic while in flight issues no second POST', async () => {
    const env = loadShare({ fetchResponses: [PRIVATE] });
    await env.settle();

    let release;
    const held = new Promise(res => { release = res; });
    env.sandbox.fetch = (url, opts) => {
        env.fetchCalls.push({ url, method: (opts && opts.method) || 'GET' });
        return held.then(() => ({ ok: true, status: 200, json: async () => PUBLIC.body }));
    };

    const first = env.sandbox.goPublic();
    env.sandbox.goPublic(); // ignored: button is busy
    release();
    await first;
    await env.settle();

    const starts = env.fetchCalls.filter(c => c.url === '/api/share/start');
    assert.equal(starts.length, 1);
});
