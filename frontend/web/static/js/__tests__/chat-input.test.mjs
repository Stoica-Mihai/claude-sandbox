// Input bar wiring + upload-then-reference tests.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FakeDocument, FakeElement } from './dom-stub.mjs';
import { createInputBar, uploadFile } from '../chat-input.js';

// async so the environment stays installed across awaits in fn (the upload
// promise renders the chip on a later microtask); all call sites await it.
async function withEnv(fn) {
    const prevDoc = globalThis.document;
    const prevWin = globalThis.window;
    const prevFetch = globalThis.fetch;
    globalThis.document = new FakeDocument();
    globalThis.window = { ROUTES: { sessionUpload: '/api/sessions/{terminalId}/upload' } };
    try {
        return await fn();
    } finally {
        globalThis.document = prevDoc;
        globalThis.window = prevWin;
        globalThis.fetch = prevFetch;
    }
}

test('sending text calls onSend with the trimmed text and clears the input', async () => {
    await withEnv(() => {
        const sent = [];
        const bar = createInputBar({ terminalId: 'claude-1', onSend: (text, path) => sent.push([text, path]) });
        const textarea = bar.el.children.find(c => c.tagName === 'TEXTAREA');
        const sendBtn = bar.el.children.find(c => c.tagName === 'BUTTON' && c.textContent === 'Send');

        textarea.value = '  hello  ';
        sendBtn.dispatch('click', {});

        assert.deepEqual(sent, [['hello', null]]);
        assert.equal(textarea.value, '');
    });
});

test('Enter (without Shift) sends; Shift+Enter does not', async () => {
    await withEnv(() => {
        const sent = [];
        const bar = createInputBar({ terminalId: 'claude-1', onSend: (text) => sent.push(text) });
        const textarea = bar.el.children.find(c => c.tagName === 'TEXTAREA');

        textarea.value = 'line one';
        let defaulted = false;
        textarea.dispatch('keydown', { key: 'Enter', shiftKey: true, preventDefault() { defaulted = true; } });
        assert.equal(sent.length, 0);
        assert.equal(defaulted, false);

        textarea.value = 'line two';
        textarea.dispatch('keydown', { key: 'Enter', shiftKey: false, preventDefault() { defaulted = true; } });
        assert.deepEqual(sent, ['line two']);
        assert.equal(defaulted, true);
    });
});

test('sending empty text with no attachment is a no-op', async () => {
    await withEnv(() => {
        const sent = [];
        const bar = createInputBar({ terminalId: 'claude-1', onSend: (text) => sent.push(text) });
        const sendBtn = bar.el.children.find(c => c.tagName === 'BUTTON' && c.textContent === 'Send');
        sendBtn.dispatch('click', {});
        assert.deepEqual(sent, []);
    });
});

test('uploadFile posts to the session upload route and returns the saved path', async () => {
    await withEnv(async () => {
        let capturedUrl, capturedBody;
        globalThis.fetch = (url, opts) => {
            capturedUrl = url;
            capturedBody = opts.body;
            return Promise.resolve({ ok: true, json: async () => ({ path: '/state/uploads/claude-1/clipboard-abcd.png' }) });
        };
        const fakeFile = { name: 'a.png' };
        const path = await uploadFile('claude-1', fakeFile);
        assert.equal(capturedUrl, '/api/sessions/claude-1/upload');
        assert.ok(capturedBody instanceof FormData);
        assert.equal(path, '/state/uploads/claude-1/clipboard-abcd.png');
    });
});

test('uploadFile throws on a non-OK response', async () => {
    await withEnv(async () => {
        globalThis.fetch = () => Promise.resolve({ ok: false, status: 400 });
        await assert.rejects(() => uploadFile('claude-1', { name: 'a.png' }));
    });
});

test('attaching a file shows a ready chip, and sending includes its path then clears the chip', async () => {
    await withEnv(async () => {
        globalThis.fetch = () => Promise.resolve({ ok: true, json: async () => ({ path: '/state/uploads/claude-1/x.png' }) });
        const sent = [];
        const bar = createInputBar({ terminalId: 'claude-1', onSend: (text, path) => sent.push([text, path]) });
        const fileInput = bar.el.children.find(c => c.tagName === 'INPUT' && c.type === 'file');
        const chip = bar.el.children.find(c => c.classList.contains('chat-input-chip'));
        const textarea = bar.el.children.find(c => c.tagName === 'TEXTAREA');
        const sendBtn = bar.el.children.find(c => c.tagName === 'BUTTON' && c.textContent === 'Send');

        fileInput.files = [{ name: 'a.png' }];
        fileInput.dispatch('change', {});
        await new Promise(r => setImmediate(r)); // let the upload promise settle

        assert.equal(chip.classList.contains('hidden'), false);
        assert.ok(chip.textContent.includes('a.png'));

        textarea.value = 'see this';
        sendBtn.dispatch('click', {});
        await new Promise(r => setImmediate(r)); // doSend awaits the (settled) upload

        assert.deepEqual(sent, [['see this', '/state/uploads/claude-1/x.png']]);
        assert.equal(chip.classList.contains('hidden'), true);
    });
});

test('sending before the upload settles still includes the path (await guards the race)', async () => {
    await withEnv(async () => {
        let resolveUpload;
        globalThis.fetch = () => new Promise((res) => { resolveUpload = () => res({ ok: true, json: async () => ({ path: '/up/late.pdf' }) }); });
        const sent = [];
        const bar = createInputBar({ terminalId: 'claude-1', onSend: (text, path) => sent.push([text, path]) });
        const fileInput = bar.el.children.find(c => c.tagName === 'INPUT' && c.type === 'file');
        const textarea = bar.el.children.find(c => c.tagName === 'TEXTAREA');
        const sendBtn = bar.el.children.find(c => c.tagName === 'BUTTON' && c.textContent === 'Send');

        fileInput.files = [{ name: 'late.pdf' }];
        fileInput.dispatch('change', {});
        textarea.value = 'attached';
        sendBtn.dispatch('click', {}); // send while upload still in flight
        assert.deepEqual(sent, []);    // held until the upload resolves

        resolveUpload();
        await new Promise(r => setImmediate(r));
        await new Promise(r => setImmediate(r));
        assert.deepEqual(sent, [['attached', '/up/late.pdf']]);
    });
});

test('a failed upload shows a failed chip and does not attach a path on send', async () => {
    await withEnv(async () => {
        globalThis.fetch = () => Promise.resolve({ ok: false, status: 500 });
        const sent = [];
        const bar = createInputBar({ terminalId: 'claude-1', onSend: (text, path) => sent.push([text, path]) });
        const fileInput = bar.el.children.find(c => c.tagName === 'INPUT' && c.type === 'file');
        const chip = bar.el.children.find(c => c.classList.contains('chat-input-chip'));
        const textarea = bar.el.children.find(c => c.tagName === 'TEXTAREA');
        const sendBtn = bar.el.children.find(c => c.tagName === 'BUTTON' && c.textContent === 'Send');

        fileInput.files = [{ name: 'x.png' }];
        fileInput.dispatch('change', {});
        await new Promise(r => setImmediate(r));
        assert.ok(chip.textContent.includes('failed'));

        textarea.value = 'text only';
        sendBtn.dispatch('click', {});
        await new Promise(r => setImmediate(r));
        assert.deepEqual(sent, [['text only', null]]);
    });
});

test('setRunning toggles the button to Stop; clicking it interrupts instead of sending', async () => {
    await withEnv(() => {
        const sent = [];
        let stopped = 0;
        const bar = createInputBar({
            terminalId: 'claude-1',
            onSend: (t) => sent.push(t),
            onStop: () => { stopped++; },
        });
        const textarea = bar.el.children.find(c => c.tagName === 'TEXTAREA');
        const btn = bar.el.children.find(c => c.tagName === 'BUTTON' && c.textContent === 'Send');

        bar.setRunning(true);
        assert.equal(btn.textContent, 'Stop');
        assert.ok(btn.classList.contains('chat-input-stop'));

        textarea.value = 'ignored while running';
        btn.dispatch('click', {});
        assert.equal(stopped, 1);
        assert.deepEqual(sent, []);

        // Enter does not send while running.
        let defaulted = false;
        textarea.dispatch('keydown', { key: 'Enter', shiftKey: false, preventDefault: () => { defaulted = true; } });
        assert.deepEqual(sent, []);

        bar.setRunning(false);
        assert.equal(btn.textContent, 'Send');
        btn.dispatch('click', {});
        assert.deepEqual(sent, ['ignored while running']);
    });
});
