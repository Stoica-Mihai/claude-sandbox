'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { loadShare } = require('./load-share');

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
    assert.equal(doc.getElementById('shareDot').classList.contains('hidden'), true);
});

test('page load with a live tunnel lights the globe without opening the modal', async () => {
    const env = loadShare({ fetchResponses: [PUBLIC] });
    await env.settle();

    const doc = env.document;
    assert.equal(doc.getElementById('shareBtn').classList.contains('share-on'), true);
    assert.equal(doc.getElementById('shareDot').classList.contains('hidden'), false);
    assert.equal(doc.getElementById('shareModal')._open, undefined);
});

test('openShareModal shows the dialog and refetches status', async () => {
    const env = loadShare({ fetchResponses: [PRIVATE, PRIVATE] });
    await env.settle();

    env.sandbox.openShareModal();
    await env.settle();

    assert.equal(env.document.getElementById('shareModal')._open, true);
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
    assert.equal(doc.getElementById('shareBtn').classList.contains('share-on'), true);
    assert.equal(doc.getElementById('shareDot').classList.contains('hidden'), false);
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
    assert.equal(doc.getElementById('shareBtn').classList.contains('share-on'), false);
    assert.equal(doc.getElementById('shareDot').classList.contains('hidden'), true);
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
