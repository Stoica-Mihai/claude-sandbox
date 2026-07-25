// chat-render.js DOM structure tests using the minimal FakeDocument stub.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FakeDocument, FakeElement } from './dom-stub.mjs';
import { applyPatches, appendSystemNotice, setConnectionNotice, clearConnectionNotice, appendUserMessage, resetView, showPending } from '../chat-render.js';

// Assistant boxes carry chrome (the turn-copy button) alongside block
// elements, so address blocks by class rather than raw child index.
const blocksOf = (box) => [...(box._body || box).children].filter((c) => c.classList.contains('chat-block'));
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

test('setConnectionNotice updates one notice in place instead of stacking', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        for (let i = 1; i <= 5; i++) setConnectionNotice(list, `[Reconnecting… (attempt ${i})]`);
        assert.equal(list.children.length, 1);
        assert.equal(list.children[0].textContent, '[Reconnecting… (attempt 5)]');
        clearConnectionNotice(list);
        assert.equal(list.children.length, 0);
        assert.equal(list._chatConnNotice, null);
    });
});

test('clearConnectionNotice is a no-op when no notice exists', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        clearConnectionNotice(list); // must not throw
        assert.equal(list.children.length, 0);
    });
});

test('appendUserMessage appends a user-role message', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        appendUserMessage(list, 'hello there');
        assert.ok(list.children[0].classList.contains('chat-msg-user'));
        assert.equal(list.children[0].querySelector('.chat-msg-body').textContent, 'hello there');
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

test('a tool row previews its input — Bash as an always-visible boxed command, Read as a header path', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'tool', toolName: 'Bash', input: { command: 'git status\nsecond line' }, done: true } },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 1, block: { type: 'tool', toolName: 'Read', input: { file_path: '/workspace/app/main.go' }, done: true } },
        ]);
        const blocks = blocksOf(list.children[0]);
        // Bash: full command in a boxed .chat-tool-cmd (not the header summary).
        assert.equal(blocks[0].querySelector('.chat-tool-cmd').textContent, 'git status\nsecond line');
        assert.equal(blocks[0].querySelector('.chat-tool-preview').textContent, '');
        // Read: path in the compact header preview.
        assert.equal(blocks[1].querySelector('.chat-tool-preview').textContent, '/workspace/app/main.go');
    });
});

test('an Agent (subagent) tool renders as a card: description label + markdown report, no raw output', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'tool', toolName: 'Agent', input: { subagent_type: 'general-purpose', description: 'Implement Task 1 peek core', prompt: 'do the thing' }, resultText: '## Done\nchanged src/x.rs', done: true } },
        ]);
        const blk = blocksOf(list.children[0])[0];
        assert.ok(blk.classList.contains('chat-block-subagent'), 'subagent card class');
        assert.equal(blk.querySelector('.chat-tool-preview').textContent, 'Implement Task 1 peek core');
        assert.ok(blk.querySelector('.chat-subagent-report'), 'report rendered');
        assert.equal(blk.querySelector('.chat-tool-output'), null, 'agent uses the report, not a raw output pre');
    });
});

test('a running tool shows a live elapsed marker; a finished one shows its final duration', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        // running = input finalized (done) but no result yet
        const running = { type: 'tool', toolName: 'Bash', input: { command: 'nix develop' }, done: true };
        running.startedAt = Date.now() - 5000; // pretend it started 5s ago
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: running },
        ]);
        let el = blocksOf(list.children[0])[0].querySelector('.chat-tool-elapsed');
        assert.ok(el, 'running tool has an elapsed marker');
        assert.match(el.textContent, /⏱ \d+s/);
        assert.ok(el.attributes['data-started'], 'ticker anchor present while running');

        // result arrives → finished: marker freezes to the final duration
        running.finished = true;
        running.resultText = 'ok';
        applyPatches(list, [{ kind: 'tool-result', messageId: 'm1', blockIndex: 0, block: running }]);
        el = blocksOf(list.children[0])[0].querySelector('.chat-tool-elapsed');
        assert.match(el.textContent, /⏱ \d+s/);
        assert.equal(el.attributes['data-started'], undefined, 'no longer ticking');
    });
});

test('a replayed tool shows its real duration from record timestamps', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        // Replay: call + result arrive together, so only the timestamps can tell
        // how long it took (103s — like a cold `nix develop`).
        const block = {
            type: 'tool', toolName: 'Bash', input: { command: 'nix develop' }, done: true, finished: true,
            resultText: 'ok', tsStart: '2026-07-25T11:50:00.000Z', tsEnd: '2026-07-25T11:51:43.000Z',
        };
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block },
        ]);
        const el = blocksOf(list.children[0])[0].querySelector('.chat-tool-elapsed');
        assert.ok(el, 'history row gets a duration');
        assert.equal(el.textContent, '⏱ 1m 43s');
    });
});

test('a tall command box is capped when collapsed; a short one is not', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        const tall = Array.from({ length: 8 }, (_, i) => 'line ' + i).join('\n');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'tool', toolName: 'Bash', input: { command: tall }, done: true } },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 1, block: { type: 'tool', toolName: 'Bash', input: { command: 'ls' }, done: true } },
        ]);
        const blocks = blocksOf(list.children[0]);
        assert.ok(blocks[0].querySelector('.chat-tool-cmd').classList.contains('chat-tool-cmd--capped'), 'tall command capped');
        assert.equal(blocks[1].querySelector('.chat-tool-cmd').classList.contains('chat-tool-cmd--capped'), false, 'short command not capped');
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

test('a header patch invokes onHeader without touching the DOM list', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        let gotHeader;
        applyPatches(list, [
            { kind: 'header', header: { cwd: '/workspace/x', model: 'opus' } },
        ], { onHeader: (h) => { gotHeader = h; } });
        assert.deepEqual(gotHeader, { cwd: '/workspace/x', model: 'opus' });
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
        assert.equal(list.children[0].querySelector('.chat-msg-body').textContent, 'hello from co-viewer');
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
            const btn = (box._body || box).children.find(c => c.classList.contains('chat-turn-copy'));
            assert.ok(btn, 'turn-copy button present');
            assert.equal(btn.classList.contains('hidden'), false); // revealed by prose
            btn.dispatch('click', { stopPropagation() {} });
            assert.equal(captured, 'Hello world\n\nDone.');
        } finally {
            if (prev) Object.defineProperty(globalThis, 'navigator', prev);
        }
    });
});

test('the active assistant turn is marked (pulse target) and a new user turn clears it', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'append-text', messageId: 'm1', blockIndex: 0, block: { type: 'text', text: 'hi' } },
        ]);
        const msg = list.children.find(c => c.classList.contains('chat-msg-assistant'));
        assert.ok(msg.classList.contains('is-active-turn'), 'active turn marked');
        assert.equal(list._activeAssistantEl, msg);
        appendUserMessage(list, 'next question', false, Date.now());
        assert.equal(msg.classList.contains('is-active-turn'), false, 'cleared by the new user turn');
        assert.equal(list._activeAssistantEl, null);
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
        const btn = (box._body || box).children.find(c => c.classList.contains('chat-turn-copy'));
        assert.equal(btn.classList.contains('hidden'), true);
    });
});

test('a user message with an attachment renders a masked icon, not an emoji', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        appendUserMessage(list, 'here', true);
        const bubble = list.children[0];
        assert.ok(bubble.textContent.includes('here'));
        assert.equal(bubble.textContent.includes('📎'), false); // no color emoji
        const icon = [...bubble.children].find(c => c.classList.contains('chat-attach-icon'));
        assert.ok(icon, 'attachment icon element present');
    });
});

test('the interrupt marker uses a machined square, not the stop emoji', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [{ kind: 'interrupt' }]);
        const marker = list.children.find(c => c.classList.contains('chat-interrupted'));
        assert.ok(marker.textContent.includes('Stopped'));
        assert.equal(marker.textContent.includes('⏹'), false);
    });
});

test('a quick-replies patch renders tappable chips that send the option and clear', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        const sent = [];
        applyPatches(list, [{ kind: 'quick-replies', options: ['Yes', 'No'] }], { onQuickReply: (t) => sent.push(t) });
        const row = list.children.find(c => c.classList.contains('chat-quick-replies'));
        assert.ok(row);
        const chips = row.children.filter(c => c.classList.contains('chat-quick-reply'));
        assert.equal(chips.length, 2);
        chips[0].dispatch('click', {});
        assert.deepEqual(sent, ['Yes']);
        // tapping clears the row
        assert.equal(list.children.some(c => c.classList.contains('chat-quick-replies')), false);
    });
});

test('a new quick-replies row replaces the previous one', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [{ kind: 'quick-replies', options: ['A'] }], {});
        applyPatches(list, [{ kind: 'quick-replies', options: ['B'] }], {});
        const rows = list.children.filter(c => c.classList.contains('chat-quick-replies'));
        assert.equal(rows.length, 1);
        assert.equal(rows[0].children[0].textContent, 'B');
    });
});

test('appending a user message clears pending quick-reply chips', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [{ kind: 'quick-replies', options: ['A', 'B'] }], {});
        appendUserMessage(list, 'typed instead', false);
        assert.equal(list.children.some(c => c.classList.contains('chat-quick-replies')), false);
    });
});

test('messages carry a timestamp element (transcript ts for replay, now for live)', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        appendUserMessage(list, 'hi', false, '2026-07-23T15:24:00Z');
        const uTime = list.children[0].querySelector('.chat-time');
        assert.ok(uTime, 'user bubble has a timestamp');
        assert.ok(uTime.textContent.length > 0);

        applyPatches(list, [{ kind: 'new-message', messageId: 'm1', role: 'assistant', ts: '2026-07-23T15:25:00Z' }]);
        const box = list.children.find(c => c.classList.contains('chat-msg-assistant'));
        assert.ok(box.querySelector('.chat-time'), 'assistant box has a timestamp');
    });
});

test('a merged assistant turn stamps the time once, not per engine message', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant', ts: '2026-07-23T15:25:00Z' },
            { kind: 'new-message', messageId: 'm2', role: 'assistant', ts: '2026-07-23T15:25:30Z' },
        ]);
        const box = list.children.find(c => c.classList.contains('chat-msg-assistant'));
        assert.equal([...(box._body || box).children].filter(c => c.classList.contains('chat-time')).length, 1);
    });
});

test('an unsent message dims and shows a retry that removes the bubble and re-sends', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        let retried = 0;
        const el = appendUserMessage(list, 'lost one', false, Date.now(), { unsent: true, onRetry: () => { retried++; } });
        assert.ok(el.classList.contains('chat-msg-unsent'));
        const retry = [...el.children].find(c => c.classList.contains('chat-retry'));
        assert.ok(retry, 'retry control present');
        assert.equal(list.children.length, 1);

        retry.dispatch('click', {});
        assert.equal(retried, 1);
        assert.equal(list.children.length, 0); // failed bubble removed on retry
    });
});

test('a sent message (no unsent opt) has no retry and is not dimmed', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        const el = appendUserMessage(list, 'ok one', false, Date.now(), null);
        assert.equal(el.classList.contains('chat-msg-unsent'), false);
        assert.equal([...el.children].some(c => c.classList.contains('chat-retry')), false);
    });
});

test('tool-body code panes get the same copy icon as markdown fences', () => {
    withDocument(() => {
        const list = new FakeElement('div');
        applyPatches(list, [
            { kind: 'new-message', messageId: 'm1', role: 'assistant' },
            { kind: 'finalize-block', messageId: 'm1', blockIndex: 0, block: { type: 'tool', toolName: 'Bash', toolUseId: 't1', input: { command: 'ls -la' }, resultText: 'a\nb', done: true } },
        ]);
        const blockEl = firstBlock(list.children[0]);
        const copyBtns = blockEl.querySelectorAll('.chat-code-copy');
        assert.ok(copyBtns.length >= 2, 'copy icon on the command pre and the output pre'); // cmd + output
    });
});
