import test from 'node:test';
import assert from 'node:assert/strict';
import { loadViews, FakeElement } from './load-views.mjs';
import { clickEvent, clickTrash } from './delete-history-helpers.mjs';

// Covers the "delete-session-history" UI wiring in views.js — dpRenderHistory
// (rebuilds the previous-sessions list in place and attaches a per-row delete
// control) and the inline two-step delete state machine (dpDelToIdle →
// dpDelToConfirm → dpDelConfirmed / dpDelFail). The control uses the Futurism kit
// component: .row-act is a div CONTAINER; the idle trash is a .row-act-btn child
// button; confirm swaps in .confirm-yes / .confirm-no buttons.

function jsonResponse(body, { ok = true, status = 200 } = {}) {
    return { ok, status, json: async () => body };
}

function historyEnv() {
    return loadViews({ mobile: false, ids: ['session-actions'] });
}

// The .row-act delete containers rendered under #session-actions.
function delControls(env) {
    const actions = env.document.getElementById('session-actions');
    const found = [];
    const walk = (el) => (el.children || []).forEach(c => {
        if (c.classList.contains('row-act')) found.push(c);
        walk(c);
    });
    walk(actions);
    return found;
}

function rowWraps(env) {
    return env.document.getElementById('session-actions').children
        .filter(c => c.classList.contains('row-host'));
}

// ---------- dpRenderHistory: rendering + delete-control wiring ----------

test('dpRenderHistory renders a delete control per previous-session row', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.resolve(jsonResponse([
        { uuid: 'aaaaaaaa-1111', created: 100, name: 'first' },
        { uuid: 'bbbbbbbb-2222', created: 200 },
    ]));

    await env.sandbox.dpRenderHistory('/workspace/proj');

    const wraps = rowWraps(env);
    assert.equal(wraps.length, 2, 'two row-host rows');
    const acts = delControls(env);
    assert.equal(acts.length, 2, 'one delete control per row');
    acts.forEach(act => {
        assert.equal(act.tagName, 'DIV', '.row-act is a div container');
        const btn = act.children.find(c => c.classList.contains('row-act-btn'));
        assert.ok(btn, 'idle trash button present');
        assert.equal(btn.title, 'Delete this conversation permanently', 'has delete tooltip');
        assert.equal(typeof btn.onclick, 'function', 'trash button has an arm-on-click handler');
    });
});

test('dpRenderHistory shows empty-state and no delete controls when there is no history', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.resolve(jsonResponse([]));
    await env.sandbox.dpRenderHistory('/p');
    const actions = env.document.getElementById('session-actions');
    assert.ok(actions.children.some(c => c.classList.contains('empty-state')), 'empty-state rendered');
    assert.equal(delControls(env).length, 0, 'no delete controls');
});

test('dpRenderHistory tolerates a rejected history fetch (renders empty-state)', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.reject(new Error('offline'));
    await env.sandbox.dpRenderHistory('/p');
    const actions = env.document.getElementById('session-actions');
    assert.ok(actions.children.some(c => c.classList.contains('empty-state')), 'falls back to empty');
});

test('dpRenderHistory tolerates a non-ok history fetch (renders empty-state)', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.resolve(jsonResponse(null, { ok: false, status: 500 }));
    await env.sandbox.dpRenderHistory('/p');
    const actions = env.document.getElementById('session-actions');
    assert.ok(actions.children.some(c => c.classList.contains('empty-state')), 'empty on non-ok');
});

test('dpRenderHistory is re-invokable: strips old rows/label/empty-state and rebuilds in place', async () => {
    const env = historyEnv();
    const actions = env.document.getElementById('session-actions');

    // Keep a "Start a new session" sibling above; it must survive re-render.
    const newRow = new FakeElement('button');
    newRow.className = 'arow sa-row';
    actions.appendChild(newRow);

    env.sandbox.fetch = () => Promise.resolve(jsonResponse([
        { uuid: 'u1', created: 1 },
        { uuid: 'u2', created: 2 },
    ]));
    await env.sandbox.dpRenderHistory('/p');
    assert.equal(rowWraps(env).length, 2, 'first render: two rows');

    env.sandbox.fetch = () => Promise.resolve(jsonResponse([{ uuid: 'u2', created: 2 }]));
    await env.sandbox.dpRenderHistory('/p');

    assert.equal(rowWraps(env).length, 1, 'rebuilt to one row');
    assert.equal(actions.children.filter(c => c.classList.contains('actitle')).length, 1,
        'exactly one label (old one stripped)');
    assert.ok(actions.children.includes(newRow), 'the Start-new row is left untouched');
});

test('dpRenderHistory is a no-op when #session-actions is absent', async () => {
    const env = loadViews({ mobile: false, ids: [] });
    await assert.doesNotReject(() => env.sandbox.dpRenderHistory('/p'));
});

// ---------- dpDelToIdle / dpDelToConfirm: inline two-step confirm ----------

test('dpDelToIdle renders the trash glyph and arms a confirm on click', () => {
    const env = historyEnv();
    const act = new FakeElement('div');
    act.classList.add('confirming', 'failed');

    env.sandbox.dpDelToIdle(act, '/p', 'u1');

    assert.equal(act.classList.contains('confirming'), false, 'confirming cleared');
    assert.equal(act.classList.contains('failed'), false, 'failed cleared');
    const btn = act.children.find(c => c.classList.contains('row-act-btn'));
    assert.ok(btn, 'trash button present');
    assert.match(btn.innerHTML, /<svg/, 'shows trash glyph');

    const e = clickTrash(act);
    assert.ok(e._stopped && e._prevented, 'idle click stops + prevents the row click');
    assert.ok(act.classList.contains('confirming'), 'first click arms the confirm');
});

test('dpDelToConfirm shows Delete + Cancel buttons in the container', () => {
    const env = historyEnv();
    const act = new FakeElement('div');

    env.sandbox.dpDelToConfirm(act, '/p', 'u1');

    assert.ok(act.classList.contains('confirming'), 'confirming class set');
    const yes = act.children.find(c => c.classList.contains('confirm-yes'));
    const no = act.children.find(c => c.classList.contains('confirm-no'));
    assert.ok(yes && yes.textContent === 'Delete', 'Delete button present');
    assert.ok(no && no.textContent === 'Cancel', 'Cancel button present');
    assert.equal(act.children.some(c => c.classList.contains('row-act-btn')), false, 'idle trash replaced');
});

test('Cancel reverts an armed delete back to idle', () => {
    const env = historyEnv();
    const act = new FakeElement('div');
    env.sandbox.dpDelToConfirm(act, '/p', 'u1');
    const no = act.children.find(c => c.classList.contains('confirm-no'));

    no.onclick(clickEvent());

    assert.equal(act.classList.contains('confirming'), false, 'back to idle');
    const btn = act.children.find(c => c.classList.contains('row-act-btn'));
    assert.ok(btn && /<svg/.test(btn.innerHTML), 'trash glyph restored');
});

// ---------- dpDelConfirmed: the DELETE request + re-render ----------

test('Confirm fires DELETE to the history endpoint and re-renders on 204', async () => {
    const env = historyEnv();
    const calls = [];
    let historyFetches = 0;
    env.sandbox.fetch = (url, opts) => {
        calls.push({ url, opts });
        if (opts && opts.method === 'DELETE') {
            return Promise.resolve({ status: 204 });
        }
        historyFetches++;
        return Promise.resolve(jsonResponse([]));
    };

    const act = new FakeElement('div');
    env.sandbox.dpDelToConfirm(act, '/workspace/p', 'dead-beef', env.sandbox.dpRenderHistory);
    const yes = act.children.find(c => c.classList.contains('confirm-yes'));

    await yes.onclick(clickEvent());
    await new Promise(r => setImmediate(r));

    const delCall = calls.find(c => c.opts && c.opts.method === 'DELETE');
    assert.ok(delCall, 'a DELETE was issued');
    assert.equal(delCall.url, '/api/sessions/history/dead-beef', 'targets the encoded uuid');
    assert.ok(historyFetches >= 1, 're-rendered history after a 204');
});

test('dpDelConfirmed encodes the uuid in the DELETE URL', async () => {
    const env = historyEnv();
    let seen = null;
    env.sandbox.fetch = (url, opts) => {
        if (opts && opts.method === 'DELETE') { seen = url; return Promise.resolve({ status: 204 }); }
        return Promise.resolve(jsonResponse([]));
    };
    await env.sandbox.dpDelConfirmed(new FakeElement('div'), '/p', 'a/b c');
    assert.equal(seen, '/api/sessions/history/' + encodeURIComponent('a/b c'));
});

// ---------- dpDelConfirmed failure paths → dpDelFail ----------

test('A rejected DELETE flashes Failed then reverts to idle', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.reject(new Error('network'));
    const act = new FakeElement('div');
    act.classList.add('confirming');

    await env.sandbox.dpDelConfirmed(act, '/p', 'u1');

    assert.equal(act.classList.contains('confirming'), false, 'confirming dropped on failure');
    assert.ok(act.classList.contains('failed'), 'failed class set');
    assert.match(act.textContent, /Failed/, 'shows Failed label');

    env.flushTimers(); // fire the revert-to-idle timeout
    assert.equal(act.classList.contains('failed'), false, 'failed cleared after timeout');
    const btn = act.children.find(c => c.classList.contains('row-act-btn'));
    assert.ok(btn && /<svg/.test(btn.innerHTML), 'trash glyph restored after revert');
});

test('A non-204 DELETE response flashes Failed and does not re-render history', async () => {
    const env = historyEnv();
    let historyFetches = 0;
    env.sandbox.fetch = (url, opts) => {
        if (opts && opts.method === 'DELETE') return Promise.resolve({ status: 500 });
        historyFetches++;
        return Promise.resolve(jsonResponse([]));
    };
    const act = new FakeElement('div');

    await env.sandbox.dpDelConfirmed(act, '/p', 'u1');

    assert.ok(act.classList.contains('failed'), 'failed flash on a 500');
    assert.equal(historyFetches, 0, 'no re-render when delete did not succeed');
});

test('dpDelFail flashes then reverts to idle after the timeout', () => {
    const env = historyEnv();
    const act = new FakeElement('div');
    act.classList.add('confirming');

    env.sandbox.dpDelFail(act, '/p', 'u1');

    assert.equal(act.classList.contains('confirming'), false);
    assert.ok(act.classList.contains('failed'));

    env.flushTimers();
    assert.equal(act.classList.contains('failed'), false, 'reverted to idle');
    const btn = act.children.find(c => c.classList.contains('row-act-btn'));
    assert.ok(btn, 'idle trash restored');
});

// ---------- end-to-end: delete the last row, list re-renders empty ----------

test('deleting the only previous session re-renders to the empty state', async () => {
    const env = historyEnv();
    let phase = 'initial';
    env.sandbox.fetch = (url, opts) => {
        if (opts && opts.method === 'DELETE') { phase = 'deleted'; return Promise.resolve({ status: 204 }); }
        return Promise.resolve(jsonResponse(phase === 'deleted' ? [] : [{ uuid: 'only', created: 5 }]));
    };

    await env.sandbox.dpRenderHistory('/p');
    assert.equal(rowWraps(env).length, 1, 'one row before delete');

    const act = delControls(env)[0];
    clickTrash(act);                            // idle → armed
    const yes = act.children.find(c => c.classList.contains('confirm-yes'));
    await yes.onclick(clickEvent());            // armed → confirmed DELETE
    await new Promise(r => setImmediate(r));

    assert.equal(rowWraps(env).length, 0, 'no rows after delete');
    const actions = env.document.getElementById('session-actions');
    assert.ok(actions.children.some(c => c.classList.contains('empty-state')), 'empty-state shown');
});

// ---------- dpRenderHistory: stale-response race ----------

// Concatenate innerHTML across the subtree (row titles are set via innerHTML,
// not textContent, so the fake DOM's textContent getter can't see them).
function collectHTML(el) {
    let s = el.innerHTML || '';
    (el.children || []).forEach(c => { s += collectHTML(c); });
    return s;
}

test('dpRenderHistory drops a stale response that resolves after a newer render', async () => {
    const env = loadViews({ mobile: false, ids: ['session-actions'] });
    const resolvers = {};
    env.sandbox.fetch = (url) => {
        const cwd = new URLSearchParams(String(url).split('?')[1]).get('cwd');
        return new Promise((resolve) => { resolvers[cwd] = resolve; });
    };

    const entriesA = [{ uuid: 'aaaaaaaa-1111-2222-3333-444444444444', created: 1700000000, name: 'from-folder-A' }];
    const entriesB = [{ uuid: 'bbbbbbbb-5555-6666-7777-888888888888', created: 1700000100, name: 'from-folder-B' }];

    // Start a render for folder A, then folder B — B supersedes A.
    const pA = env.sandbox.dpRenderHistory('/workspace/A');
    const pB = env.sandbox.dpRenderHistory('/workspace/B');

    // The newer render (B) resolves first, then the stale one (A) lands late.
    resolvers['/workspace/B']({ ok: true, status: 200, json: async () => entriesB });
    await pB;
    resolvers['/workspace/A']({ ok: true, status: 200, json: async () => entriesA });
    await pA;

    const html = collectHTML(env.document.getElementById('session-actions'));
    assert.ok(html.includes('from-folder-B'), 'newer folder B rows present');
    assert.ok(!html.includes('from-folder-A'), 'stale folder A rows dropped');
});
