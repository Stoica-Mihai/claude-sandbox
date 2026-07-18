import test from 'node:test';
import assert from 'node:assert/strict';
import { loadViews, FakeElement } from './load-views.mjs';

// The client session store: single source of truth for the server session
// list, fed by the #session-data JSON embedded in the sessions fragment.
// These tests pin that the tab bar / badge render from the store, not from
// scraped sidebar card markup.

function sessionJSON(list) {
    const el = new FakeElement('script');
    el.textContent = JSON.stringify(list);
    return el;
}

function storeEnv(sessions, ids = []) {
    const env = loadViews({ mobile: false, ids });
    env.document.register('session-data', sessionJSON(sessions));
    env.sandbox.readSessionsFromDOM();
    return env;
}

function swapSessions(env, sessions) {
    env.document.getElementById('session-data').textContent = JSON.stringify(sessions);
    env.document.dispatch('htmx:afterSwap', { target: { id: 'session-list' } });
}

// ---------- store: parse + notify ----------

test('readSessionsFromDOM parses the embedded payload', () => {
    const env = storeEnv([{ name: 't1', display_name: 'proj' }]);
    assert.equal(env.sandbox.getSessions().length, 1);
    assert.equal(env.sandbox.getSession('t1').display_name, 'proj');
    assert.equal(env.sandbox.getSession('nope'), null);
});

test('session-list afterSwap re-reads the payload and notifies subscribers', () => {
    const env = storeEnv([]);
    const seen = [];
    env.sandbox.subscribe(s => seen.push(s.length));

    swapSessions(env, [{ name: 't1', display_name: 'a' }, { name: 't2', display_name: 'b' }]);

    assert.deepEqual(seen, [2]);
    assert.equal(env.sandbox.getSessions().length, 2);
});

test('afterSwap of an unrelated target does not touch the store', () => {
    const env = storeEnv([{ name: 't1', display_name: 'a' }]);
    env.document.getElementById('session-data').textContent = '[]';
    env.document.dispatch('htmx:afterSwap', { target: { id: 'dir-picker' } });
    assert.equal(env.sandbox.getSessions().length, 1);
});

test('malformed payload keeps the last known state', () => {
    const env = storeEnv([{ name: 't1', display_name: 'a' }]);
    env.document.getElementById('session-data').textContent = '{not json';
    env.document.dispatch('htmx:afterSwap', { target: { id: 'session-list' } });
    assert.equal(env.sandbox.getSessions().length, 1);
});

// ---------- tab bar renders from the store ----------

function tabEnv(sessions) {
    return storeEnv(sessions, ['singleTerminal', 'singleTabBar']);
}

test('tab label comes from the store display_name', () => {
    const env = tabEnv([{ name: 't1', display_name: 'my-project' }]);
    env.sandbox.openSessionSingle('t1');

    const bar = env.document.getElementById('singleTabBar');
    assert.ok(bar.innerHTML.includes('my-project'), 'label rendered from store');
    assert.ok(!bar.innerHTML.includes('Session ended'), 'live session has no dead marker');
});

test('rename propagates to the tab label on the next session-list swap', () => {
    const env = tabEnv([{ name: 't1', display_name: 'old-name' }]);
    env.sandbox.openSessionSingle('t1');

    swapSessions(env, [{ name: 't1', display_name: 'new-name' }]);

    const bar = env.document.getElementById('singleTabBar');
    assert.ok(bar.innerHTML.includes('new-name'), 'renamed label rendered');
    assert.ok(!bar.innerHTML.includes('old-name'), 'stale label gone');
});

test('session gone from the list marks the tab dead but keeps its label', () => {
    const env = tabEnv([{ name: 't1', display_name: 'proj' }]);
    env.sandbox.openSessionSingle('t1');

    swapSessions(env, []);

    const bar = env.document.getElementById('singleTabBar');
    assert.ok(bar.innerHTML.includes('Session ended'), 'dead marker rendered');
    assert.ok(bar.innerHTML.includes('proj'), 'last-known label kept');
});

// ---------- header badge follows the store ----------

test('badge text and alive class follow the session count', () => {
    const env = storeEnv([], ['session-badge-text']);
    const badge = env.document.getElementById('session-badge-text');

    swapSessions(env, [{ name: 't1', display_name: 'a' }]);
    assert.equal(badge.textContent, '1 session');
    assert.ok(badge.classList.contains('alive'));

    swapSessions(env, []);
    assert.equal(badge.textContent, '0 sessions');
    assert.equal(badge.classList.contains('alive'), false);
});
