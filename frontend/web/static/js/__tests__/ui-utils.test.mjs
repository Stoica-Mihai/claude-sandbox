// Covers the shared fetch helpers in ui-utils.js: sendJSON must always set the
// JSON Content-Type (a caller once dropped it — the bug this pins) and
// errorText must surface the backend {error} envelope with a fallback.

import test from 'node:test';
import assert from 'node:assert/strict';
import { sendJSON, errorText } from '../ui-utils.js';

test('sendJSON always sets Content-Type, method, and a stringified body', async () => {
    let captured = null;
    globalThis.fetch = (url, opts) => { captured = { url, opts }; return Promise.resolve({ ok: true }); };

    await sendJSON('/api/thing', 'PUT', { name: 'x' });

    assert.equal(captured.url, '/api/thing');
    assert.equal(captured.opts.method, 'PUT');
    assert.equal(captured.opts.headers['Content-Type'], 'application/json');
    assert.equal(captured.opts.body, JSON.stringify({ name: 'x' }));
});

test('errorText returns the backend error message when present', async () => {
    const res = { status: 400, json: async () => ({ error: 'bad name' }) };
    assert.equal(await errorText(res, 'fallback'), 'bad name');
});

test('errorText falls back when the body has no error field', async () => {
    const res = { status: 500, json: async () => ({}) };
    assert.equal(await errorText(res, 'save failed'), 'save failed');
});

test('errorText falls back when the body is not JSON', async () => {
    const res = { status: 502, json: async () => { throw new Error('not json'); } };
    assert.equal(await errorText(res, 'save failed'), 'save failed');
});

test('errorText uses a generic status line when no fallback is given', async () => {
    const res = { status: 503, json: async () => null };
    assert.equal(await errorText(res), 'Request failed (503)');
});
