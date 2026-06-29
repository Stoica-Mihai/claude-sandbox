'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { loadViews, FakeElement } = require('./load-views');

// These tests pin the "frontend-css-single-source-cleanup" change: inline
// style writes were replaced with class/text toggles in three state paths —
// the mobile select overlay, the rename-error flash, and the spawn-fail button.
// They assert the class is the ONLY mechanism (no inline color writes survive).

// ---------- mobileToggleSelect: sel-active class on the button ----------

function fakeTerminalInstance() {
    const lineStubs = {
        0: { translateToString: () => 'line one' },
        1: { translateToString: () => 'line two' },
    };
    return {
        term: {
            rows: 1,
            buffer: { active: { baseY: 0, getLine: (i) => lineStubs[i] || null } },
            focus() {},
            scrollToBottom() {},
        },
    };
}

function envWithActiveTerminal() {
    const env = loadViews({
        mobile: true,
        ids: ['singleTerminal'],
    });
    const inst = fakeTerminalInstance();
    env.sandbox.TerminalManager.get = () => inst;
    // Drive the active-tab state (singleTerminalId) through the public open path,
    // since views.js's let-scoped state is not reachable from the sandbox.
    env.sandbox.openSessionSingle('term-1');
    return env;
}

test('mobileToggleSelect adds sel-active class (not inline color) on open', () => {
    const env = envWithActiveTerminal();
    const btn = new FakeElement('button');

    env.sandbox.mobileToggleSelect(btn);

    assert.ok(btn.classList.contains('sel-active'), 'sel-active class added');
    assert.equal(btn.style.color, '', 'no inline color written');
    assert.equal(btn.style.borderColor, '', 'no inline borderColor written');

    const terminal = env.document.getElementById('singleTerminal');
    const overlay = terminal.children.find(c => c.id === 'selectOverlay');
    assert.ok(overlay, 'select overlay created');
    assert.equal(overlay.textContent, 'line one\nline two', 'overlay holds visible lines');
});

test('mobileToggleSelect removes sel-active class and overlay on second toggle', () => {
    const env = envWithActiveTerminal();
    const btn = new FakeElement('button');

    env.sandbox.mobileToggleSelect(btn); // open
    // Re-register the created overlay so getElementById('selectOverlay') resolves.
    const terminal = env.document.getElementById('singleTerminal');
    const overlay = terminal.children.find(c => c.id === 'selectOverlay');
    env.document.register('selectOverlay', overlay);

    env.sandbox.mobileToggleSelect(btn); // close

    assert.equal(btn.classList.contains('sel-active'), false, 'sel-active class removed');
    assert.equal(btn.style.color, '', 'still no inline color');
    assert.equal(overlay.parentNode, null, 'overlay removed from DOM');
});

test('mobileToggleSelect is a no-op when no terminal element exists', () => {
    const env = loadViews({ mobile: true, ids: [] });
    const btn = new FakeElement('button');
    assert.doesNotThrow(() => env.sandbox.mobileToggleSelect(btn));
    assert.equal(btn.classList.contains('sel-active'), false, 'button untouched');
});

test('mobileToggleSelect does not open an overlay when no active terminal', () => {
    const env = loadViews({ mobile: true, ids: ['singleTerminal'] });
    const btn = new FakeElement('button');
    env.sandbox.mobileToggleSelect(btn);
    const terminal = env.document.getElementById('singleTerminal');
    assert.equal(terminal.children.length, 0, 'no overlay created');
    assert.equal(btn.classList.contains('sel-active'), false, 'no sel-active without a session');
});

test('mobileToggleSelect tolerates a missing button on close path', () => {
    const env = envWithActiveTerminal();
    env.sandbox.mobileToggleSelect(new FakeElement('button'));
    const terminal = env.document.getElementById('singleTerminal');
    const overlay = terminal.children.find(c => c.id === 'selectOverlay');
    env.document.register('selectOverlay', overlay);
    assert.doesNotThrow(() => env.sandbox.mobileToggleSelect(null));
});

// ---------- openSessionSingle: term-tab class (replaces absolute inset-0) ----------

test('openSessionSingle gives the tab container the term-tab class, not utility classes', () => {
    const env = loadViews({ mobile: false, ids: ['singleTerminal'] });
    const created = [];
    env.sandbox.TerminalManager.create = (id, container) => created.push(container);

    env.sandbox.openSessionSingle('term-xyz');

    const terminal = env.document.getElementById('singleTerminal');
    const tab = terminal.children.find(c => c.id === 'singleTab-term-xyz');
    assert.ok(tab, 'tab container created');
    assert.ok(tab.classList.contains('term-tab'), 'has term-tab class');
    assert.ok(tab.classList.contains('hidden'), 'starts hidden');
    assert.ok(tab.classList.contains('terminal-bg'), 'keeps terminal-bg class');
    assert.equal(tab.classList.contains('absolute'), false, 'no leftover absolute utility class');
    assert.equal(tab.classList.contains('inset-0'), false, 'no leftover inset-0 utility class');
});

// ---------- rename-error: err-flash class (replaces inline outline) ----------

function renameEnv() {
    return loadViews({
        mobile: false,
        ids: ['renameSubmit', 'renameInput', 'renameModal'],
    });
}

test('rename failure adds err-flash class (not inline outline) on the input', async () => {
    const env = renameEnv();
    env.sandbox.fetch = () => Promise.resolve({ ok: false, status: 500 });
    env.document.getElementById('renameInput').value = 'newname';

    // Set rename target via the modal opener.
    env.sandbox.openRenameModal('term-1', 'old');
    env.document.getElementById('renameSubmit').dispatch('click', {});

    await new Promise(r => setImmediate(r)); // let the rejected fetch settle

    const input = env.document.getElementById('renameInput');
    assert.ok(input.classList.contains('err-flash'), 'err-flash class added');
    assert.equal(input.style.outline, '', 'no inline outline written');

    env.flushTimers(); // fire the 2s reset
    assert.equal(input.classList.contains('err-flash'), false, 'err-flash removed after timeout');
});

test('rename network error (rejected fetch) also flashes via class', async () => {
    const env = renameEnv();
    env.sandbox.fetch = () => Promise.reject(new Error('offline'));
    env.sandbox.openRenameModal('term-2', '');
    env.document.getElementById('renameSubmit').dispatch('click', {});
    await new Promise(r => setImmediate(r));

    const input = env.document.getElementById('renameInput');
    assert.ok(input.classList.contains('err-flash'), 'err-flash on rejection');
});

test('rename success does not flash and closes the modal', async () => {
    const env = renameEnv();
    let closed = false;
    env.sandbox.fetch = () => Promise.resolve({ ok: true, status: 200 });
    const modal = env.document.getElementById('renameModal');
    modal.close = () => { closed = true; };
    env.sandbox.openRenameModal('term-3', 'x');
    env.document.getElementById('renameSubmit').dispatch('click', {});
    await new Promise(r => setImmediate(r));

    assert.equal(env.document.getElementById('renameInput').classList.contains('err-flash'), false);
    assert.equal(closed, true, 'modal closed on success');
});

// ---------- spawn-fail: btn-spawn-fail class + text (replaces inline bg/color) ----------

function spawnEnv() {
    return loadViews({
        mobile: false,
        ids: ['dir-picker-submit', 'newSessionModal'],
    });
}

function afterRequestEvent(status, terminalId) {
    return {
        detail: {
            xhr: {
                status,
                getResponseHeader: (h) => (h === 'X-Terminal-Id' ? terminalId : null),
            },
        },
    };
}

test('spawn failure adds btn-spawn-fail class and label (not inline bg/color)', () => {
    const env = spawnEnv();
    const btn = env.document.getElementById('dir-picker-submit');

    env.document.dispatch('htmx:afterRequest', afterRequestEvent(500, 'term-1'));

    assert.ok(btn.classList.contains('btn-spawn-fail'), 'btn-spawn-fail class added');
    assert.equal(btn.textContent, 'Failed — try again', 'failure label set');
    assert.equal(btn.style.background, '', 'no inline background written');
    assert.equal(btn.style.color, '', 'no inline color written');
});

test('spawn-fail resets to Launch label and drops the class after timeout', () => {
    const env = spawnEnv();
    const btn = env.document.getElementById('dir-picker-submit');

    env.document.dispatch('htmx:afterRequest', afterRequestEvent(500, 'term-1'));
    env.flushTimers();

    assert.equal(btn.classList.contains('btn-spawn-fail'), false, 'class removed');
    assert.equal(btn.textContent, 'Launch', 'default label restored');
});

test('spawn-fail restores Resume label when a resume was selected', () => {
    const env = spawnEnv();
    // Select a resume through the public setter (dirPickerSel is let-scoped).
    env.sandbox.dirPickerSetSel('resume', 'u1', null);
    const btn = env.document.getElementById('dir-picker-submit');

    env.document.dispatch('htmx:afterRequest', afterRequestEvent(503, 'term-1'));
    env.flushTimers();

    assert.equal(btn.textContent, 'Resume', 'resume label restored');
});

test('successful spawn (2xx) closes the modal, opens the session, no fail flag', () => {
    const env = spawnEnv();
    let opened = null;
    let closed = false;
    env.sandbox.openSession = (id) => { opened = id; };
    env.document.getElementById('newSessionModal').close = () => { closed = true; };
    const btn = env.document.getElementById('dir-picker-submit');

    env.document.dispatch('htmx:afterRequest', afterRequestEvent(200, 'term-ok'));

    assert.equal(btn.classList.contains('btn-spawn-fail'), false, 'no fail class on success');
    assert.equal(closed, true, 'new-session modal closed');
    assert.equal(opened, 'term-ok', 'opened the spawned terminal');
});

test('afterRequest without X-Terminal-Id header is ignored (not a spawn response)', () => {
    const env = spawnEnv();
    const btn = env.document.getElementById('dir-picker-submit');
    btn.textContent = 'Launch';

    env.document.dispatch('htmx:afterRequest', afterRequestEvent(500, null));

    assert.equal(btn.classList.contains('btn-spawn-fail'), false, 'untouched when no terminal id');
    assert.equal(btn.textContent, 'Launch', 'label untouched');
});
