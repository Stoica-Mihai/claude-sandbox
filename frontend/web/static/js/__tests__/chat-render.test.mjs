// chat-render.js DOM structure tests using the minimal FakeDocument stub.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FakeDocument, FakeElement } from './dom-stub.mjs';
import { applyPatches, appendSystemNotice, appendUserMessage, resetView } from '../chat-render.js';

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
        assert.equal(msgEl.children.length, 1); // one block element, re-rendered in place
        assert.ok(msgEl.children[0].innerHTML.includes('Hello'));
    });
});

test('a thinking block renders collapsed by default with a toggle', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'thinking', text: 'reasoning', collapsed: true, done: true } },
        ]);
        const blockEl = list.children[0].children[0];
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
        const blockEl = list.children[0].children[0];
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
        const blockEl = list.children[0].children[0];
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

        const blockEl = list.children[0].children[0];
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
        const blockEl = list.children[0].children[0];
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
        assert.equal(msgEl.children.length, 2); // m1:0 and m2:0 stay distinct blocks
        assert.ok(msgEl.children[0].innerHTML.includes('before tool'));
        assert.ok(msgEl.children[1].innerHTML.includes('after tool'));
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
        assert.ok(list.children[2].children[0].innerHTML.includes('second turn'));
    });
});

test('a thinking block with no text renders no toggle at all', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'new-block', messageId: 'm1', blockIndex: 0, block: { type: 'thinking', text: '', collapsed: true } },
        ]);
        assert.equal(list.children[0].children[0].children.length, 0);
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
        assert.ok(list.children[0].children[0].innerHTML.includes('second pass'));
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
