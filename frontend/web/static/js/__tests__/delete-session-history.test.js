'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { loadViews, FakeElement } = require('./load-views');

// These tests pin the "delete-session-history" change. The resume-row delete
// affordance is pure CSS state (app.css): .arow-wrap wraps the row; .arow-del is
// the overlaid delete control; .confirming / .failed are its two transient state
// classes; .adel-yes / .adel-no are the inline confirm/cancel buttons. app.css is
// a stylesheet with no runtime, so we assert through views.js — the only producer
// of these class names — that every state transition lands on the documented class
// (and only the class, never an inline style that would shadow the stylesheet).

// dpRenderHistory reads #session-actions and queries it for prior rows to strip;
// the FakeElement querySelectorAll returns [] so the first render starts clean.
function historyEnv(entries) {
    const env = loadViews({ mobile: false, ids: ['session-actions'] });
    env.sandbox.fetch = (url) => {
        if (String(url).startsWith('/api/sessions/history?cwd=')) {
            return Promise.resolve({ ok: true, status: 200, json: async () => entries });
        }
        return Promise.resolve({ ok: true, status: 204, json: async () => ({}) });
    };
    return env;
}

function sampleEntries() {
    return [
        { uuid: 'aaaaaaaa-1111-2222-3333-444444444444', created: 1700000000, name: 'first' },
        { uuid: 'bbbbbbbb-5555-6666-7777-888888888888', created: 1700000100, name: '' },
    ];
}

function actionRows(env) {
    return env.document.getElementById('session-actions').children;
}

// ---------- dpRenderHistory: .arow-wrap + .arow-del scaffold ----------

test('dpRenderHistory wraps each entry in .arow-wrap with an .arow-del control', async () => {
    const env = historyEnv(sampleEntries());

    await env.sandbox.dpRenderHistory('/workspace/proj');

    const wraps = actionRows(env).filter(c => c.classList.contains('arow-wrap'));
    assert.equal(wraps.length, 2, 'one .arow-wrap per history entry');

    wraps.forEach(wrap => {
        const row = wrap.children.find(c => c.classList.contains('sa-row'));
        const del = wrap.children.find(c => c.classList.contains('arow-del'));
        assert.ok(row, '.sa-row present (the .arow-del overlay anchors to it via .arow-wrap)');
        assert.ok(del, '.arow-del delete control present');
        assert.equal(del.tagName, 'BUTTON', '.arow-del is a real button (overlay, not nested in the row button)');
    });
});

test('dpRenderHistory renders the empty-state (no .arow-wrap) when there is no history', async () => {
    const env = historyEnv([]);

    await env.sandbox.dpRenderHistory('/workspace/empty');

    const rows = actionRows(env);
    assert.equal(rows.filter(c => c.classList.contains('arow-wrap')).length, 0, 'no delete rows');
    assert.ok(rows.some(c => c.classList.contains('empty-state')), 'empty-state shown instead');
});

test('a failed (non-ok) history fetch still renders empty-state, never an .arow-del', async () => {
    const env = historyEnv([]);
    env.sandbox.fetch = () => Promise.reject(new Error('offline'));

    await env.sandbox.dpRenderHistory('/workspace/x');

    const rows = actionRows(env);
    assert.equal(rows.filter(c => c.classList.contains('arow-del')).length, 0);
    assert.ok(rows.some(c => c.classList.contains('empty-state')));
});

// ---------- dpDelToIdle: idle state is class-free of the transient states ----------

test('dpDelToIdle clears the confirming + failed state classes', () => {
    const env = historyEnv([]);
    const del = new FakeElement('button');
    del.classList.add('arow-del', 'confirming', 'failed');

    env.sandbox.dpDelToIdle(del, '/p', 'u1');

    assert.equal(del.classList.contains('confirming'), false, 'confirming cleared');
    assert.equal(del.classList.contains('failed'), false, 'failed cleared');
    assert.equal(del.style.background, '', 'no inline background — the stylesheet owns the idle look');
    assert.equal(del.style.color, '', 'no inline color');
});

// ---------- dpDelToConfirm: .confirming + .adel-yes/.adel-no children ----------

test('clicking idle .arow-del arms .confirming with .adel-yes / .adel-no children', () => {
    const env = historyEnv([]);
    const del = new FakeElement('button');
    env.sandbox.dpDelToIdle(del, '/p', 'u1');

    del.onclick({ stopPropagation() {}, preventDefault() {} });

    assert.ok(del.classList.contains('confirming'), '.confirming applied on first click');
    const yes = del.children.find(c => c.classList.contains('adel-yes'));
    const no = del.children.find(c => c.classList.contains('adel-no'));
    assert.ok(yes, '.adel-yes confirm button present');
    assert.ok(no, '.adel-no cancel button present');
    assert.equal(yes.textContent, 'Delete');
    assert.equal(no.textContent, 'Cancel');
    assert.equal(del.style.background, '', 'confirming look comes from the class, not inline style');
});

test('.adel-no (cancel) reverts the control to idle — drops .confirming and its children', () => {
    const env = historyEnv([]);
    const del = new FakeElement('button');
    env.sandbox.dpDelToConfirm(del, '/p', 'u1');

    const no = del.children.find(c => c.classList.contains('adel-no'));
    no.onclick({ stopPropagation() {}, preventDefault() {} });

    assert.equal(del.classList.contains('confirming'), false, '.confirming removed on cancel');
    assert.equal(del.classList.contains('failed'), false, 'no .failed on cancel');
    assert.match(del.innerHTML, /<svg/, 'idle trash glyph restored (replaces the .adel-yes/.adel-no markup)');
});

// ---------- dpDelConfirmed: 204 re-renders, no .failed ----------

test('.adel-yes on a 204 delete re-renders the list (no .failed flash)', async () => {
    const env = historyEnv(sampleEntries());
    await env.sandbox.dpRenderHistory('/workspace/proj');

    const before = actionRows(env).filter(c => c.classList.contains('arow-wrap')).length;
    assert.equal(before, 2);

    // After deleting, the server reports one fewer entry on the re-render fetch.
    env.sandbox.fetch = (url) => {
        if (String(url).startsWith('/api/sessions/history?cwd=')) {
            return Promise.resolve({ ok: true, status: 200, json: async () => sampleEntries().slice(1) });
        }
        return Promise.resolve({ ok: true, status: 204 });
    };

    const del = new FakeElement('button');
    await env.sandbox.dpDelConfirmed(del, '/workspace/proj', 'aaaaaaaa-1111-2222-3333-444444444444');

    assert.equal(del.classList.contains('failed'), false, 'no .failed on a successful 204');
    const after = actionRows(env).filter(c => c.classList.contains('arow-wrap')).length;
    assert.equal(after, 1, 'list re-rendered with one fewer .arow-wrap');
});

// ---------- dpDelFail: .failed flash, then revert to idle ----------

test('a non-204 delete flips .arow-del to .failed (drops .confirming), then reverts to idle', async () => {
    const env = historyEnv(sampleEntries());
    env.sandbox.fetch = (url) => {
        if (String(url).startsWith('/api/sessions/history?cwd=')) {
            return Promise.resolve({ ok: true, status: 200, json: async () => sampleEntries() });
        }
        return Promise.resolve({ ok: false, status: 500 });
    };
    const del = new FakeElement('button');
    del.classList.add('arow-del', 'confirming');

    await env.sandbox.dpDelConfirmed(del, '/p', 'u1');

    assert.ok(del.classList.contains('failed'), '.failed applied on a 500');
    assert.equal(del.classList.contains('confirming'), false, '.confirming dropped on failure');
    assert.equal(del.style.background, '', 'failure look is the class, not inline style');

    env.flushTimers(); // fire the 1800ms revert
    assert.equal(del.classList.contains('failed'), false, '.failed cleared on revert to idle');
});

test('a rejected (network-error) delete also flips to .failed', async () => {
    const env = historyEnv(sampleEntries());
    env.sandbox.fetch = () => Promise.reject(new Error('offline'));
    const del = new FakeElement('button');
    del.classList.add('arow-del', 'confirming');

    await env.sandbox.dpDelConfirmed(del, '/p', 'u1');

    assert.ok(del.classList.contains('failed'), '.failed applied on a rejected fetch');
    assert.equal(del.classList.contains('confirming'), false, '.confirming dropped');
});

test('dpDelFail reverts to a clean idle control after its timeout (no lingering state class)', () => {
    const env = historyEnv([]);
    const del = new FakeElement('button');
    del.classList.add('arow-del');

    env.sandbox.dpDelFail(del, '/p', 'u1');
    assert.ok(del.classList.contains('failed'));

    env.flushTimers();
    assert.equal(del.classList.contains('failed'), false);
    assert.equal(del.classList.contains('confirming'), false);
});
