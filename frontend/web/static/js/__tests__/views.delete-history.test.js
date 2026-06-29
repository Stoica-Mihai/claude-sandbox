'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { loadViews, FakeElement } = require('./load-views');

// Covers the "delete-session-history" change: the delete-history UI wiring in
// views.js — dpRenderHistory (now rebuilds the previous-sessions list in place
// and attaches a per-row delete button) and the inline two-step delete state
// machine (dpDelToIdle → dpDelToConfirm → dpDelConfirmed / dpDelFail).

// A click event that records whether the handler stopped/prevented it.
function clickEvent() {
    const e = {
        _stopped: false,
        _prevented: false,
        stopPropagation() { this._stopped = true; },
        preventDefault() { this._prevented = true; },
    };
    return e;
}

// JSON Response double for fetch.
function jsonResponse(body, { ok = true, status = 200 } = {}) {
    return { ok, status, json: async () => body };
}

function historyEnv() {
    return loadViews({ mobile: false, ids: ['session-actions'] });
}

// Find the delete buttons (.arow-del) rendered under #session-actions.
function delButtons(env) {
    const actions = env.document.getElementById('session-actions');
    const found = [];
    const walk = (el) => (el.children || []).forEach(c => {
        if (c.classList.contains('arow-del')) found.push(c);
        walk(c);
    });
    walk(actions);
    return found;
}

function rowWraps(env) {
    return env.document.getElementById('session-actions').children
        .filter(c => c.classList.contains('arow-wrap'));
}

// ---------- dpRenderHistory: rendering + delete-button wiring ----------

test('dpRenderHistory renders a delete button per previous-session row', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.resolve(jsonResponse([
        { uuid: 'aaaaaaaa-1111', created: 100, name: 'first' },
        { uuid: 'bbbbbbbb-2222', created: 200 },
    ]));

    await env.sandbox.dpRenderHistory('/workspace/proj');

    const wraps = rowWraps(env);
    assert.equal(wraps.length, 2, 'two arow-wrap rows');
    const dels = delButtons(env);
    assert.equal(dels.length, 2, 'one delete button per row');
    dels.forEach(d => {
        assert.ok(d.classList.contains('arow-del'), 'has arow-del class');
        assert.equal(d.title, 'Delete this conversation permanently', 'has delete tooltip');
        assert.equal(typeof d.onclick, 'function', 'starts armed with an idle click handler');
    });
});

test('dpRenderHistory header count reflects the number of entries', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.resolve(jsonResponse([
        { uuid: 'u1', created: 1 },
    ]));
    await env.sandbox.dpRenderHistory('/p');
    const actions = env.document.getElementById('session-actions');
    const title = actions.children.find(c => c.classList.contains('actitle'));
    assert.ok(title, 'actitle label rendered');
    assert.match(title.innerHTML, /Previous sessions/);
    assert.match(title.innerHTML, />1</, 'count shows 1');
});

test('dpRenderHistory shows empty-state and no delete buttons when there is no history', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.resolve(jsonResponse([]));
    await env.sandbox.dpRenderHistory('/p');
    const actions = env.document.getElementById('session-actions');
    assert.ok(actions.children.some(c => c.classList.contains('empty-state')), 'empty-state rendered');
    assert.equal(delButtons(env).length, 0, 'no delete buttons');
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

    // Re-render with a single remaining entry (as after a delete).
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
    const del = new FakeElement('button');
    del.classList.add('confirming', 'failed');

    env.sandbox.dpDelToIdle(del, '/p', 'u1');

    assert.equal(del.classList.contains('confirming'), false, 'confirming cleared');
    assert.equal(del.classList.contains('failed'), false, 'failed cleared');
    assert.match(del.innerHTML, /<svg/, 'shows trash glyph');

    const e = clickEvent();
    del.onclick(e);
    assert.ok(e._stopped && e._prevented, 'idle click stops + prevents the row click');
    assert.ok(del.classList.contains('confirming'), 'first click arms the confirm');
});

test('dpDelToConfirm shows Delete + Cancel buttons and blanks the trigger', () => {
    const env = historyEnv();
    const del = new FakeElement('button');

    env.sandbox.dpDelToConfirm(del, '/p', 'u1');

    assert.ok(del.classList.contains('confirming'), 'confirming class set');
    assert.equal(del.innerHTML, '', 'inner glyph cleared before appending controls');
    const yes = del.children.find(c => c.classList.contains('adel-yes'));
    const no = del.children.find(c => c.classList.contains('adel-no'));
    assert.ok(yes && yes.textContent === 'Delete', 'Delete button present');
    assert.ok(no && no.textContent === 'Cancel', 'Cancel button present');

    // The wrapper's own click is swallowed while armed.
    const e = clickEvent();
    del.onclick(e);
    assert.ok(e._stopped && e._prevented, 'armed wrapper swallows clicks');
});

test('Cancel reverts an armed delete back to idle', () => {
    const env = historyEnv();
    const del = new FakeElement('button');
    env.sandbox.dpDelToConfirm(del, '/p', 'u1');
    const no = del.children.find(c => c.classList.contains('adel-no'));

    no.onclick(clickEvent());

    assert.equal(del.classList.contains('confirming'), false, 'back to idle');
    assert.match(del.innerHTML, /<svg/, 'trash glyph restored');
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
        return Promise.resolve(jsonResponse([])); // post-delete: empty list
    };

    const del = new FakeElement('button');
    env.sandbox.dpDelToConfirm(del, '/workspace/p', 'dead-beef');
    const yes = del.children.find(c => c.classList.contains('adel-yes'));

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
    await env.sandbox.dpDelConfirmed(new FakeElement('button'), '/p', 'a/b c');
    assert.equal(seen, '/api/sessions/history/' + encodeURIComponent('a/b c'));
});

// ---------- dpDelConfirmed failure paths → dpDelFail ----------

test('A rejected DELETE flashes Failed then reverts to idle', async () => {
    const env = historyEnv();
    env.sandbox.fetch = () => Promise.reject(new Error('network'));
    const del = new FakeElement('button');
    del.classList.add('confirming');

    await env.sandbox.dpDelConfirmed(del, '/p', 'u1');

    assert.equal(del.classList.contains('confirming'), false, 'confirming dropped on failure');
    assert.ok(del.classList.contains('failed'), 'failed class set');
    assert.match(del.textContent, /Failed/, 'shows Failed label');

    env.flushTimers(); // fire the revert-to-idle timeout
    assert.equal(del.classList.contains('failed'), false, 'failed cleared after timeout');
    assert.match(del.innerHTML, /<svg/, 'trash glyph restored after revert');
});

test('A non-204 DELETE response flashes Failed and does not re-render history', async () => {
    const env = historyEnv();
    let historyFetches = 0;
    env.sandbox.fetch = (url, opts) => {
        if (opts && opts.method === 'DELETE') return Promise.resolve({ status: 500 });
        historyFetches++;
        return Promise.resolve(jsonResponse([]));
    };
    const del = new FakeElement('button');

    await env.sandbox.dpDelConfirmed(del, '/p', 'u1');

    assert.ok(del.classList.contains('failed'), 'failed flash on a 500');
    assert.equal(historyFetches, 0, 'no re-render when delete did not succeed');
});

test('dpDelFail swallows clicks while flashing and reverts after the timeout', () => {
    const env = historyEnv();
    const del = new FakeElement('button');
    del.classList.add('confirming');

    env.sandbox.dpDelFail(del, '/p', 'u1');

    assert.equal(del.classList.contains('confirming'), false);
    assert.ok(del.classList.contains('failed'));
    const e = clickEvent();
    del.onclick(e);
    assert.ok(e._stopped && e._prevented, 'clicks swallowed during the fail flash');

    env.flushTimers();
    assert.equal(del.classList.contains('failed'), false, 'reverted to idle');
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

    const del = delButtons(env)[0];
    del.onclick(clickEvent());                  // idle → armed
    const yes = del.children.find(c => c.classList.contains('adel-yes'));
    await yes.onclick(clickEvent());            // armed → confirmed DELETE
    await new Promise(r => setImmediate(r));

    assert.equal(rowWraps(env).length, 0, 'no rows after delete');
    const actions = env.document.getElementById('session-actions');
    assert.ok(actions.children.some(c => c.classList.contains('empty-state')), 'empty-state shown');
});
