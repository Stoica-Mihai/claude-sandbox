// surface.js switchTo() DOM-toggle + LogsManager-lifecycle tests. Exercises the
// getElementById-driven core (surfaces, sub-label, kick, sidebar bodies); the
// .main class and nav-click binding use document.querySelector*, which the stub
// doesn't match — those paths are covered by the real-app CDP verification.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FakeDocument, FakeElement } from './dom-stub.mjs';
import { switchTo } from '../surface.js';
import { LogsManager } from '../logs.js';

function withShell(fn) {
    const prevDoc = globalThis.document;
    const prevWin = globalThis.window;
    const prevEvt = globalThis.Event;
    const doc = new FakeDocument();
    const mk = (id, cls) => { const e = new FakeElement('div'); if (cls) e.className = cls; return doc.register(id, e); };
    mk('surface-dashboard', 'surface');
    mk('surface-logs', 'surface hidden');
    mk('surfaceSub'); mk('sideKick');
    mk('session-list', 'list');
    mk('logs-side', 'list hidden');
    globalThis.document = doc;
    globalThis.window = { dispatchEvent() {} };
    globalThis.Event = class { constructor(t) { this.type = t; } };

    const created = []; const destroyed = { n: 0 };
    const realCreate = LogsManager.create, realDestroy = LogsManager.destroy, realGet = LogsManager.get;
    LogsManager.create = (elm) => { created.push(elm); };
    LogsManager.get = () => null;
    LogsManager.destroy = () => { destroyed.n++; };
    try {
        return fn({ doc, created, destroyed });
    } finally {
        LogsManager.create = realCreate; LogsManager.destroy = realDestroy; LogsManager.get = realGet;
        globalThis.document = prevDoc; globalThis.window = prevWin; globalThis.Event = prevEvt;
    }
}

test('switchTo("logs") shows logs, hides dashboard, updates labels + starts LogsManager', () => {
    withShell(({ doc, created }) => {
        switchTo('logs');
        assert.ok(doc.getElementById('surface-dashboard').classList.contains('hidden'));
        assert.ok(!doc.getElementById('surface-logs').classList.contains('hidden'));
        assert.ok(doc.getElementById('session-list').classList.contains('hidden'));
        assert.ok(!doc.getElementById('logs-side').classList.contains('hidden'));
        assert.equal(doc.getElementById('surfaceSub').textContent, 'logs');
        assert.equal(doc.getElementById('sideKick').textContent, 'Logs');
        assert.equal(created.length, 1);
        assert.equal(created[0], doc.getElementById('surface-logs'));
    });
});

test('switchTo("dashboard") shows dashboard, hides logs, updates labels + stops LogsManager', () => {
    withShell(({ doc, destroyed }) => {
        switchTo('logs');
        switchTo('dashboard');
        assert.ok(!doc.getElementById('surface-dashboard').classList.contains('hidden'));
        assert.ok(doc.getElementById('surface-logs').classList.contains('hidden'));
        assert.ok(!doc.getElementById('session-list').classList.contains('hidden'));
        assert.ok(doc.getElementById('logs-side').classList.contains('hidden'));
        assert.equal(doc.getElementById('surfaceSub').textContent, 'dashboard');
        assert.equal(doc.getElementById('sideKick').textContent, 'Sessions');
        assert.equal(destroyed.n, 1);
    });
});

test('switchTo("logs") is a no-op when the logs surface is absent', () => {
    withShell(({ doc, created }) => {
        doc._byId.delete('surface-logs');
        switchTo('logs');
        assert.equal(created.length, 0);
        assert.ok(!doc.getElementById('surface-dashboard').classList.contains('hidden'));
    });
});
