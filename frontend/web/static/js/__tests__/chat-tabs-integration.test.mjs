// tabs.js kind-aware surface selection + mode-switch delegation, using the
// shared loadViews fixture (real tabs.js/picker.js/chat.js wiring, stubbed
// TerminalManager/ChatManager so no real xterm/socket is created).
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { loadViews, FakeElement } from './load-views.mjs';

function seedSession(env, session) {
    const el = new FakeElement('script');
    el.textContent = JSON.stringify([session]);
    env.document.register('session-data', el);
    env.sandbox.readSessionsFromDOM();
}

function baseIds() {
    return ['singleTerminal', 'singleChat', 'singleWelcome', 'singleControls', 'mobileInputBar', 'session-data'];
}

test('openSession creates a chat surface (not a terminal) for a kind=chat session', () => {
    const env = loadViews({ ids: baseIds() });
    seedSession(env, { name: 'claude-c1', kind: 'chat', cwd: '/workspace/x', display_name: 'x', created_at: new Date().toISOString(), alive: true });

    let created = null;
    // Only override create(): get() stays the default null-returning stub, so
    // focusActive()'s `if (instance)` guard skips safely (matching the app's
    // own contract — a real ChatManager.create() return value isn't a plain {}).
    Object.assign(env.sandbox.ChatManager, { create: (id) => { created = id; } });
    Object.assign(env.sandbox.TerminalManager, { create: () => { throw new Error('must not create a terminal for a chat session'); } });

    env.sandbox.openSession('claude-c1');

    assert.equal(created, 'claude-c1');
});

test('openSession creates a terminal (not a chat surface) for a kind=terminal session', () => {
    const env = loadViews({ ids: baseIds() });
    seedSession(env, { name: 'claude-t1', kind: 'terminal', cwd: '/workspace/x', display_name: 'x', created_at: new Date().toISOString(), alive: true });

    let created = null;
    Object.assign(env.sandbox.TerminalManager, { create: (id) => { created = id; } });
    Object.assign(env.sandbox.ChatManager, { create: () => { throw new Error('must not create a chat surface for a terminal session'); } });

    env.sandbox.openSession('claude-t1');

    assert.equal(created, 'claude-t1');
});

test('updateSingleWelcome hides the terminal-only controls for a shown chat session', () => {
    const env = loadViews({ ids: baseIds() });
    env.sandbox.updateSingleWelcome(true, 'chat');

    assert.equal(env.document.getElementById('singleChat').classList.contains('hidden'), false);
    assert.equal(env.document.getElementById('singleTerminal').classList.contains('hidden'), true);
    assert.equal(env.document.getElementById('singleControls').classList.contains('hidden'), true);
    assert.equal(env.document.getElementById('mobileInputBar').classList.contains('hidden'), true);
});

test('updateSingleWelcome shows the terminal for a shown terminal session', () => {
    const env = loadViews({ ids: baseIds() });
    env.sandbox.updateSingleWelcome(true, 'terminal');

    assert.equal(env.document.getElementById('singleTerminal').classList.contains('hidden'), false);
    assert.equal(env.document.getElementById('singleChat').classList.contains('hidden'), true);
});

test("the 'mode-switch' action posts to the mode-switch route and opens the resulting session", async () => {
    const env = loadViews({ ids: baseIds() });
    seedSession(env, { name: 'claude-new', kind: 'chat', cwd: '/workspace/x', display_name: 'x', created_at: new Date().toISOString(), alive: true });

    // requestModeSwitch is a plain (non-method) export, not reachable through
    // sandbox the way ChatManager's methods are — mock the fetch it makes
    // internally instead (fetch, unlike an ES import binding, is read from
    // global scope, so overriding it here is visible to the real module code).
    let capturedUrl, capturedBody;
    env.sandbox.fetch = (url, opts) => {
        capturedUrl = url;
        capturedBody = JSON.parse(opts.body);
        return Promise.resolve({ ok: true, json: async () => ({ session_name: 'claude-new' }) });
    };

    let opened = null;
    Object.assign(env.sandbox.ChatManager, { create: (id) => { opened = id; } });

    // actions.js's delegated click listener isn't part of loadViews' INIT_ORDER
    // (no existing view test needs it), so install it here to exercise the
    // real data-action → handler dispatch path rather than calling the
    // registered handler directly.
    env.sandbox.initActions();

    const el = new FakeElement('button');
    el.dataset.action = 'mode-switch';
    el.dataset.terminalId = 'claude-old';
    el.dataset.targetKind = 'chat';
    env.document.body.appendChild(el);

    env.document.dispatch('click', { target: el });
    await new Promise(r => setImmediate(r));

    assert.equal(capturedUrl, '/api/sessions/claude-old/mode');
    assert.deepEqual(capturedBody, { kind: 'chat' });
    assert.equal(opened, 'claude-new');
});

test('mode-switch opens the respawned session as the target kind even when the store has not caught up', async () => {
    const env = loadViews({ ids: baseIds() });
    // Deliberately NO seedSession for 'claude-new': the sidebar fragment (and
    // thus the client store) lags the mode-switch response, so kind must come
    // from the action's target-kind, not a store lookup.
    env.sandbox.fetch = () => Promise.resolve({ ok: true, json: async () => ({ session_name: 'claude-new' }) });

    let opened = null;
    Object.assign(env.sandbox.ChatManager, { create: (id) => { opened = id; } });
    Object.assign(env.sandbox.TerminalManager, { create: () => { throw new Error('stale store must not force a terminal surface'); } });

    env.sandbox.initActions();
    const el = new FakeElement('button');
    el.dataset.action = 'mode-switch';
    el.dataset.terminalId = 'claude-old';
    el.dataset.targetKind = 'chat';
    env.document.body.appendChild(el);

    env.document.dispatch('click', { target: el });
    await new Promise(r => setImmediate(r));

    assert.equal(opened, 'claude-new');
});
