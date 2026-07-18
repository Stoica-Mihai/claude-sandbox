'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { loadViews, FakeElement } = require('./load-views');
const { clickEvent, clickTrash } = require('./delete-history-helpers');

// These tests pin the "delete-session-history" change. The resume-row delete
// affordance uses the Futurism kit component (futurism.css): .row-host wraps the
// row; .row-act is the overlaid delete CONTAINER (a div, so its button children
// never nest inside the row button); .confirming / .failed are its transient
// state classes; the idle trash is a .row-act-btn child; .confirm-yes / .confirm-no
// are the inline confirm/cancel buttons. We assert through views.js — the producer
// of this markup — that each state transition lands on the documented class (and
// never an inline style that would shadow the stylesheet).

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

// ---------- dpRenderHistory: .row-host + .row-act scaffold ----------

test('dpRenderHistory wraps each entry in .row-host with a .row-act control', async () => {
    const env = historyEnv(sampleEntries());

    await env.sandbox.dpRenderHistory('/workspace/proj');

    const wraps = actionRows(env).filter(c => c.classList.contains('row-host'));
    assert.equal(wraps.length, 2, 'one .row-host per history entry');

    wraps.forEach(wrap => {
        const row = wrap.children.find(c => c.classList.contains('sa-row'));
        const act = wrap.children.find(c => c.classList.contains('row-act'));
        assert.ok(row, '.sa-row present (the .row-act overlay anchors to it via .row-host)');
        assert.ok(act, '.row-act delete control present');
        assert.equal(act.tagName, 'DIV', '.row-act is a div container (its button children never nest in a button)');
        assert.ok(act.children.find(c => c.classList.contains('row-act-btn')), 'idle .row-act-btn trash present');
    });
});

test('dpRenderHistory renders the empty-state (no .row-host) when there is no history', async () => {
    const env = historyEnv([]);

    await env.sandbox.dpRenderHistory('/workspace/empty');

    const rows = actionRows(env);
    assert.equal(rows.filter(c => c.classList.contains('row-host')).length, 0, 'no delete rows');
    assert.ok(rows.some(c => c.classList.contains('empty-state')), 'empty-state shown instead');
});

test('a failed (non-ok) history fetch still renders empty-state, never a .row-act', async () => {
    const env = historyEnv([]);
    env.sandbox.fetch = () => Promise.reject(new Error('offline'));

    await env.sandbox.dpRenderHistory('/workspace/x');

    const rows = actionRows(env);
    assert.equal(rows.filter(c => c.classList.contains('row-act')).length, 0);
    assert.ok(rows.some(c => c.classList.contains('empty-state')));
});

// ---------- dpDelToIdle: idle state is class-free of the transient states ----------

test('dpDelToIdle clears the confirming + failed state classes and shows the trash', () => {
    const env = historyEnv([]);
    const act = new FakeElement('div');
    act.classList.add('row-act', 'confirming', 'failed');

    env.sandbox.dpDelToIdle(act, '/p', 'u1');

    assert.equal(act.classList.contains('confirming'), false, 'confirming cleared');
    assert.equal(act.classList.contains('failed'), false, 'failed cleared');
    const btn = act.children.find(c => c.classList.contains('row-act-btn'));
    assert.ok(btn, 'idle trash button present');
    assert.match(btn.innerHTML, /<svg/, 'trash glyph rendered');
    assert.equal(act.style.background, '', 'no inline background — the stylesheet owns the idle look');
});

// ---------- dpDelToConfirm: .confirming + .confirm-yes/.confirm-no children ----------

test('clicking the idle trash arms .confirming with .confirm-yes / .confirm-no children', () => {
    const env = historyEnv([]);
    const act = new FakeElement('div');
    env.sandbox.dpDelToIdle(act, '/p', 'u1');

    clickTrash(act);

    assert.ok(act.classList.contains('confirming'), '.confirming applied on first click');
    const yes = act.children.find(c => c.classList.contains('confirm-yes'));
    const no = act.children.find(c => c.classList.contains('confirm-no'));
    assert.ok(yes, '.confirm-yes confirm button present');
    assert.ok(no, '.confirm-no cancel button present');
    assert.equal(yes.textContent, 'Delete');
    assert.equal(no.textContent, 'Cancel');
    assert.equal(act.style.background, '', 'confirming look comes from the class, not inline style');
});

test('.confirm-no (cancel) reverts the control to idle — drops .confirming, restores the trash', () => {
    const env = historyEnv([]);
    const act = new FakeElement('div');
    env.sandbox.dpDelToConfirm(act, '/p', 'u1');

    const no = act.children.find(c => c.classList.contains('confirm-no'));
    no.onclick(clickEvent());

    assert.equal(act.classList.contains('confirming'), false, '.confirming removed on cancel');
    assert.equal(act.classList.contains('failed'), false, 'no .failed on cancel');
    const btn = act.children.find(c => c.classList.contains('row-act-btn'));
    assert.ok(btn && /<svg/.test(btn.innerHTML), 'idle trash glyph restored');
});

// ---------- dpDelConfirmed: 204 re-renders, no .failed ----------

test('confirm on a 204 delete re-renders the list (no .failed flash)', async () => {
    const env = historyEnv(sampleEntries());
    await env.sandbox.dpRenderHistory('/workspace/proj');

    const before = actionRows(env).filter(c => c.classList.contains('row-host')).length;
    assert.equal(before, 2);

    env.sandbox.fetch = (url) => {
        if (String(url).startsWith('/api/sessions/history?cwd=')) {
            return Promise.resolve({ ok: true, status: 200, json: async () => sampleEntries().slice(1) });
        }
        return Promise.resolve({ ok: true, status: 204 });
    };

    const act = new FakeElement('div');
    await env.sandbox.dpDelConfirmed(act, '/workspace/proj', 'aaaaaaaa-1111-2222-3333-444444444444');

    assert.equal(act.classList.contains('failed'), false, 'no .failed on a successful 204');
    const after = actionRows(env).filter(c => c.classList.contains('row-host')).length;
    assert.equal(after, 1, 'list re-rendered with one fewer .row-host');
});

// ---------- dpDelFail: .failed flash, then revert to idle ----------

test('a non-204 delete flips .row-act to .failed (drops .confirming), then reverts to idle', async () => {
    const env = historyEnv(sampleEntries());
    env.sandbox.fetch = (url) => {
        if (String(url).startsWith('/api/sessions/history?cwd=')) {
            return Promise.resolve({ ok: true, status: 200, json: async () => sampleEntries() });
        }
        return Promise.resolve({ ok: false, status: 500 });
    };
    const act = new FakeElement('div');
    act.classList.add('row-act', 'confirming');

    await env.sandbox.dpDelConfirmed(act, '/p', 'u1');

    assert.ok(act.classList.contains('failed'), '.failed applied on a 500');
    assert.equal(act.classList.contains('confirming'), false, '.confirming dropped on failure');
    assert.equal(act.style.background, '', 'failure look is the class, not inline style');

    env.flushTimers(); // fire the 1800ms revert
    assert.equal(act.classList.contains('failed'), false, '.failed cleared on revert to idle');
});

test('a rejected (network-error) delete also flips to .failed', async () => {
    const env = historyEnv(sampleEntries());
    env.sandbox.fetch = () => Promise.reject(new Error('offline'));
    const act = new FakeElement('div');
    act.classList.add('row-act', 'confirming');

    await env.sandbox.dpDelConfirmed(act, '/p', 'u1');

    assert.ok(act.classList.contains('failed'), '.failed applied on a rejected fetch');
    assert.equal(act.classList.contains('confirming'), false, '.confirming dropped');
});

test('dpDelFail reverts to a clean idle control after its timeout (no lingering state class)', () => {
    const env = historyEnv([]);
    const act = new FakeElement('div');
    act.classList.add('row-act');

    env.sandbox.dpDelFail(act, '/p', 'u1');
    assert.ok(act.classList.contains('failed'));

    env.flushTimers();
    assert.equal(act.classList.contains('failed'), false);
    assert.equal(act.classList.contains('confirming'), false);
});
