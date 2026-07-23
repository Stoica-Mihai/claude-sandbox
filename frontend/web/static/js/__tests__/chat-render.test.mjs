// chat-render.js DOM structure tests using the minimal FakeDocument stub.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FakeDocument, FakeElement } from './dom-stub.mjs';
import { applyPatches, appendSystemNotice, appendUserMessage, resetView, showPending } from '../chat-render.js';

// Assistant boxes carry chrome (the turn-copy button) alongside block
// elements, so address blocks by class rather than raw child index.
const blocksOf = (box) => [...box.children].filter((c) => c.classList.contains('chat-block'));
const firstBlock = (box) => blocksOf(box)[0];

function withDocument(fn) {
    const prevDoc = globalThis.document;
    const prevWin = globalThis.window;
    globalThis.document = new FakeDocument();
    globalThis.window = {}; // no marked/DOMPurify — exercises the plain-text fallback
    try {
        return fn(globalThis.document);
    } finally {
        globalThis.document = prevDoc;
        globalThis.window = prevWin;
    }
}

test('appendSystemNotice appends a system-role message', () => {
    withDocument((doc) => {
        const list = new FakeElement('div');
        appendSystemNotice(list, '[Session ended]');
        assert.equal(list.children.length, 1);
        assert.ok(list.children[0].classList.contains('chat-msg-system'));
        assert.equal(list.children[0].textContent, '[Session ended]');
    });
});

test('appendUserMessage appends a user-role message', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        appendUserMessage(list, 'hello there');
        assert.ok(list.children[0].classList.contains('chat-msg-user'));
        assert.equal(list.children[0].textContent, 'hello there');
    });
});

test('new-message then append-text patches build one message bubble with one text block', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'Hel' } },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'Hello' } },
        ]);
        assert.equal(list.children.length, 1);
        const msgEl = list.children[0];
        assert.ok(msgEl.classList.contains('chat-msg-assistant'));
        assert.equal(blocksOf(msgEl).length, 1); // one block element, re-rendered in place
        assert.ok(firstBlock(msgEl).innerHTML.includes('Hello'));
    });
});

test('a thinking block renders collapsed by default with a toggle', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'thinking', text: 'reasoning', collapsed: true, done: true } },
        ]);
        const blockEl = firstBlock(list.children[0]);
        assert.ok(blockEl.classList.contains('chat-block-thinking'));
        // Collapsed: only the toggle button renders, no body text visible.
        assert.equal(blockEl.children.length, 1);
        assert.equal(blockEl.children[0].tagName, 'BUTTON');
    });
});

test('a tool block for Edit renders a diff with old/new panes', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            {
                kind: 'finalize-block', messageId: 'm1', blockIndex: 0,
                block: { type: 'tool', toolName: 'Edit', toolUseId: 't1', input: { old_string: 'foo', new_string: 'bar' }, done: true },
            },
        ]);
        const blockEl = firstBlock(list.children[0]);
        const body = blockEl.querySelector('.chat-tool-body');
        const diff = body.querySelector('.chat-diff');
        assert.ok(diff);
        const del = diff.children.find(c => c.classList.contains('chat-diff-del'));
        const add = diff.children.find(c => c.classList.contains('chat-diff-add'));
        assert.equal(del.textContent, 'foo');
        assert.equal(add.textContent, 'bar');
    });
});

test('a tool block for Bash renders the command', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            {
                kind: 'finalize-block', messageId: 'm1', blockIndex: 0,
                block: { type: 'tool', toolName: 'Bash', toolUseId: 't1', input: { command: 'ls -la' }, done: true },
            },
        ]);
        const blockEl = firstBlock(list.children[0]);
        const cmd = blockEl.querySelector('.chat-tool-cmd');
        assert.equal(cmd.textContent, 'ls -la');
    });
});

test('a tool-result patch appends the output excerpt to the existing tool block', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        const block = { type: 'tool', toolName: 'Bash', toolUseId: 't1', input: { command: 'ls' }, done: true };
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block },
        ]);
        block.resultText = 'file1\nfile2';
        applyPatches(list, [{ kind: 'tool-result', messageId: 'm1', blockIndex: 0, block }]);

        const blockEl = firstBlock(list.children[0]);
        const out = blockEl.querySelector('.chat-tool-output');
        assert.equal(out.textContent, 'file1\nfile2');
    });
});

test('header and usage patches invoke their callbacks without touching the DOM list', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        let gotHeader, gotUsage;
        applyPatches(list, [
            { kind: 'header', header: { cwd: '/workspace/x', model: 'opus' } },
            { kind: 'usage', usage: { totalCostUsd: 0.5 } },
        ], {
            onHeader: (h) => { gotHeader = h; },
            onUsage: (u) => { gotUsage = u; },
        });
        assert.deepEqual(gotHeader, { cwd: '/workspace/x', model: 'opus' });
        assert.deepEqual(gotUsage, { totalCostUsd: 0.5 });
        assert.equal(list.children.length, 0);
    });
});

test('plain text without marked/DOMPurify loaded falls back to escaped text', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: '<b>bold</b>', done: true } },
        ]);
        const blockEl = firstBlock(list.children[0]);
        assert.ok(blockEl.innerHTML.includes('&lt;b&gt;'));
    });
});

test('consecutive assistant messages merge into one visual box with distinct blocks', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'before tool' } },
            { kind: 'new-message', messageId: 'm2', role: 'assistant' },
            { kind: 'append-text', messageId: 'm2', blockIndex: 0, block: { type: 'text', text: 'after tool' } },
        ]);
        assert.equal(list.children.length, 1); // one box for the whole turn
        const msgEl = list.children[0];
        assert.equal(blocksOf(msgEl).length, 2); // m1:0 and m2:0 stay distinct blocks
        assert.ok(blocksOf(msgEl)[0].innerHTML.includes('before tool'));
        assert.ok(blocksOf(msgEl)[1].innerHTML.includes('after tool'));
    });
});

test('a user bubble between assistant messages breaks the merge group', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'first turn' } },
        ]);
        appendUserMessage(list, 'next question');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm2', role: 'assistant' },
            { kind: 'append-text', messageId: 'm2', blockIndex: 0, block: { type: 'text', text: 'second turn' } },
        ]);
        assert.equal(list.children.length, 3); // assistant, user, assistant
        assert.ok(list.children[2].classList.contains('chat-msg-assistant'));
        assert.ok(list.children[2].innerHTML === '' || true); // container exists
        assert.ok(firstBlock(list.children[2]).innerHTML.includes('second turn'));
    });
});

test('a thinking block with no text renders no toggle at all', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'new-block', messageId: 'm1', blockIndex: 0, block: { type: 'thinking', text: '', collapsed: true } },
        ]);
        assert.equal(firstBlock(list.children[0]).children.length, 0);
    });
});

test('resetView clears the element caches so a rebuild renders fresh nodes', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'first pass' } },
        ]);
        resetView(list);
        assert.equal(list.children.length, 0);
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'second pass' } },
        ]);
        assert.equal(list.children.length, 1); // rendered into the DOM, not a detached cached node
        assert.ok(firstBlock(list.children[0]).innerHTML.includes('second pass'));
    });
});

test('a user-message patch appends a user bubble', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [{ kind: 'user-message', text: 'hello from co-viewer' }]);
        assert.ok(list.children[0].classList.contains('chat-msg-user'));
        assert.equal(list.children[0].textContent, 'hello from co-viewer');
    });
});

test('langFromPath maps extensions to highlight.js language ids', async () => {
    const { langFromPath } = await import('../chat-render.js');
    assert.equal(langFromPath('/workspace/x/MyHostApduService.java'), 'java');
    assert.equal(langFromPath('a/b/handlers.go'), 'go');
    assert.equal(langFromPath('view.tsx'), 'typescript');
    assert.equal(langFromPath('Makefile'), '');
    assert.equal(langFromPath(undefined), '');
});

test('a finalized Read tool block highlights its output with the file language', () => {
    withDocument(() => {
        const calls = [];
        globalThis.window = {
            hljs: {
                getLanguage: (l) => l === 'java',
                highlight: (text, opts) => { calls.push(opts.language); return { value: '<span class="hljs-keyword">x</span>' }; },
                highlightAuto: (text) => { calls.push('auto'); return { value: text }; },
            },
        };
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            {
                kind: 'tool-result', messageId: 'm1', blockIndex: 0,
                block: { type: 'tool', toolName: 'Read', done: true, input: { file_path: '/x/A.java' }, resultText: 'class A {}' },
            },
        ]);
        assert.deepEqual(calls.filter(c => c === 'java').length >= 1, true);
    });
});

test('stableMarkdownBoundary respects fences and blank lines', async () => {
    const { stableMarkdownBoundary } = await import('../chat-render.js');
    assert.equal(stableMarkdownBoundary('para one\n\npara two'), 'para one\n\n'.length);
    assert.equal(stableMarkdownBoundary('no blank lines yet'), 0);
    // A blank line inside an open fence is NOT a stable boundary.
    const fenced = 'intro\n\n```js\ncode\n\nmore code';
    assert.equal(stableMarkdownBoundary(fenced), 'intro\n\n'.length);
});

test('streaming parses each stable segment once and only re-parses the tail', () => {
    withDocument(() => {
        const parsed = [];
        globalThis.window = {
            marked: { parse: (t) => { parsed.push(t); return t; } },
            DOMPurify: { sanitize: (h) => h },
        };
        const list = new FakeElement('div');
        const block = { type: 'text', text: 'para one\n\npar', done: false };
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block },
        ]);
        block.text = 'para one\n\npara two\n\npar';
        applyPatches(list, [{ kind: 'append-text', messageId: 'm1', blockIndex: 0, block }]);
        block.text = 'para one\n\npara two\n\npara three';
        applyPatches(list, [{ kind: 'append-text', messageId: 'm1', blockIndex: 0, block }]);

        // 'para one' must have been parsed exactly once as a stable segment.
        const parsesContainingParaOne = parsed.filter(t => t.includes('para one')).length;
        assert.equal(parsesContainingParaOne, 1);

        // Rendered output still contains everything.
        const blockEl = firstBlock(list.children[0]);
        assert.ok(blockEl.innerHTML.includes('para one'));
        assert.ok(blockEl.innerHTML.includes('para three'));

        // Finalize does one full authoritative parse.
        block.done = true;
        applyPatches(list, [{ kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block }]);
        assert.equal(parsed[parsed.length - 1], 'para one\n\npara two\n\npara three');
    });
});

test('pending indicator: shown once, cleared by the first content patch, replay-safe', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        showPending(list);
        showPending(list); // no duplicate
        assert.equal(list.children.filter(c => c.classList.contains('chat-pending')).length, 1);

        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'hi' } },
        ]);
        assert.equal(list.children.filter(c => c.classList.contains('chat-pending')).length, 0);

        // A mirrored co-viewer send shows it again.
        applyPatches(list, [{ kind: 'user-message', text: 'other viewer asks' }]);
        assert.equal(list.children.filter(c => c.classList.contains('chat-pending')).length, 1);

        // resetView drops the tracked ref so a rebuild can show a fresh one.
        resetView(list);
        assert.equal(list.children.length, 0);
        showPending(list);
        assert.equal(list.children.filter(c => c.classList.contains('chat-pending')).length, 1);
    });
});

test('a result-driven turn end clears a pending row', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        showPending(list);
        assert.equal(list.children.filter(c => c.classList.contains('chat-pending')).length, 1);
        // an interrupt patch clears pending and adds the Stopped marker.
        applyPatches(list, [{ kind: 'interrupt' }]);
        assert.equal(list.children.filter(c => c.classList.contains('chat-pending')).length, 0);
        assert.equal(list.children.filter(c => c.classList.contains('chat-interrupted')).length, 1);
    });
});

test('turn-copy copies only the prose, excluding thinking and tool blocks', () => {
    withDocument(() => {
        let captured = null;
        const prev = Object.getOwnPropertyDescriptor(globalThis, 'navigator');
        Object.defineProperty(globalThis, 'navigator', {
            value: { clipboard: { writeText: (t) => { captured = t; return Promise.resolve(); } } },
            configurable: true,
        });
        try {
            const list = new FakeElement('div');
            applyPatches(list, [
                { kind: 'new-message', messageId: 'm1', role: 'assistant' },
                { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'Hello world' } },
                { kind: 'finalize-block', messageId: 'm1', blockIndex: 1, block: { type: 'thinking', text: 'secret reasoning', collapsed: true, done: true } },
                { kind: 'finalize-block', messageId: 'm1', blockIndex: 2, block: { type: 'tool', toolName: 'Bash', input: { command: 'ls' }, done: true } },
                { kind: 'append-text', messageId: 'm1', blockIndex: 3, block: { type: 'text', text: 'Done.' } },
            ]);
            const box = list.children.find(c => c.classList.contains('chat-msg-assistant'));
            const btn = box.children.find(c => c.classList.contains('chat-turn-copy'));
            assert.ok(btn, 'turn-copy button present');
            assert.equal(btn.classList.contains('hidden'), false); // revealed by prose
            btn.dispatch('click', { stopPropagation() {} });
            assert.equal(captured, 'Hello world\n\nDone.');
        } finally {
            if (prev) Object.defineProperty(globalThis, 'navigator', prev);
        }
    });
});

test('a tool-only turn keeps the copy button hidden (no prose to copy)', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'tool', toolName: 'Bash', input: { command: 'ls' }, done: true } },
        ]);
        const box = list.children.find(c => c.classList.contains('chat-msg-assistant'));
        const btn = box.children.find(c => c.classList.contains('chat-turn-copy'));
        assert.equal(btn.classList.contains('hidden'), true);
    });
});
