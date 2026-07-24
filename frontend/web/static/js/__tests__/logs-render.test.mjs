// logs-render.js DOM structure tests using the minimal FakeDocument stub.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { FakeDocument } from './dom-stub.mjs';
import { makeRow, appendRow, prependRow, clearRows, showNote, renderStatus } from '../logs-render.js';

function withDocument(fn) {
    const prev = globalThis.document;
    globalThis.document = new FakeDocument();
    try { return fn(globalThis.document); }
    finally { globalThis.document = prev; }
}

const rec = (o) => Object.assign({ ts: '2026-07-24T10:47:11.164Z', service: 'backend', level: 'info', msg: 'hi' }, o);

test('makeRow: level + error classes and column text', () => {
    withDocument(() => {
        const row = makeRow(rec({ level: 'error', msg: 'boom', attrs: { cwd: '/x' } }));
        assert.ok(row.classList.contains('lz-row'));
        assert.ok(row.classList.contains('lvl-error'));
        assert.ok(row.classList.contains('err'));
        const [time, svc, lvl, msg] = row.children;
        assert.ok(time.classList.contains('lz-time'));
        assert.equal(svc.textContent, 'backend');
        assert.equal(lvl.textContent, 'ERROR');
        // message text + muted attrs span both present.
        assert.match(msg.textContent, /boom/);
        assert.match(msg.textContent, /cwd=\/x/);
        assert.ok([...msg.children].some((c) => c.classList.contains('lz-attr')));
    });
});

test('makeRow: raw record renders RAW with no err edge for info-less line', () => {
    withDocument(() => {
        const row = makeRow({ ts: 't', service: 'backend', level: 'error', raw: 'goroutine 42' });
        assert.ok(row.classList.contains('err')); // raw is error-level
        const lvl = row.children[2];
        assert.equal(lvl.textContent, 'RAW');
        assert.equal(row.children[3].textContent, 'goroutine 42');
    });
});

test('appendRow adds at the bottom, prependRow at the top (chronological order)', () => {
    withDocument(() => {
        const list = document.createElement('div');
        appendRow(list, rec({ msg: 'second' }));
        prependRow(list, rec({ msg: 'first' }));
        appendRow(list, rec({ msg: 'third' }));
        const msgs = list.children.map((r) => r.children[3].textContent);
        assert.deepEqual(msgs, ['first', 'second', 'third']);
    });
});

test('clearRows empties the list; showNote paints an unavailable state', () => {
    withDocument(() => {
        const list = document.createElement('div');
        appendRow(list, rec());
        clearRows(list);
        assert.equal(list.children.length, 0);

        showNote(list, 'Log service unavailable.', true);
        assert.equal(list.children.length, 1);
        assert.ok(list.children[0].classList.contains('lz-note'));
        assert.ok(list.children[0].classList.contains('err'));
        assert.equal(list.children[0].textContent, 'Log service unavailable.');
    });
});

test('renderStatus: up chip = ink dot + last-seen; down chip = accent + downtime', () => {
    withDocument(() => {
        const now = Date.parse('2026-07-24T10:47:11.000Z');
        const strip = document.createElement('div');
        renderStatus(strip, [
            { service: 'backend', state: 'up', lastLogSeen: '2026-07-24T10:47:09.000Z' },
            { service: 'frontend', state: 'down', since: '2026-07-24T10:47:05.000Z' },
            { service: 'logd', state: 'up' }, // excluded
        ], now);

        // logd excluded, ordered sessiond(absent)→backend→frontend→...
        const chips = strip.children;
        assert.equal(chips.length, 2);
        const backend = chips.find((c) => c.dataset.service === 'backend');
        const frontend = chips.find((c) => c.dataset.service === 'frontend');
        assert.ok(backend.classList.contains('up'));
        assert.match(backend.textContent, /2s ago/);
        assert.ok(frontend.classList.contains('down'));
        assert.match(frontend.textContent, /down 6s/);
    });
});

test('renderStatus: an SSE transition flips a chip up → down', () => {
    withDocument(() => {
        const strip = document.createElement('div');
        const chip = () => strip.children.find((c) => c.dataset.service === 'backend');

        renderStatus(strip, [{ service: 'backend', state: 'up', lastLogSeen: new Date().toISOString() }], Date.now());
        assert.ok(chip().classList.contains('up'));
        assert.ok(!chip().classList.contains('down'));

        // Next snapshot (as delivered over /api/status/stream) marks it down.
        renderStatus(strip, [{ service: 'backend', state: 'down', since: new Date().toISOString() }], Date.now());
        assert.ok(chip().classList.contains('down'));
        assert.ok(!chip().classList.contains('up'));
    });
});
