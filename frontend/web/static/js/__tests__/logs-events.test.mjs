// Pure log-record / status translation + filter-predicate tests (no DOM).
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
    matchesFilter, recordKey, isRaw, levelClass, fmtTime, fmtAttrs,
    toRow, toChip, orderChips, STATUS_ORDER,
} from '../logs-events.js';

const rec = (o) => Object.assign({ ts: '2026-07-24T10:47:11.164Z', service: 'backend', level: 'info', msg: 'hello world' }, o);

test('matchesFilter: all/empty filters match everything', () => {
    assert.equal(matchesFilter(rec(), {}), true);
    assert.equal(matchesFilter(rec(), { service: 'all', level: 'all', q: '' }), true);
});

test('matchesFilter: exact service (case-insensitive)', () => {
    assert.equal(matchesFilter(rec({ service: 'backend' }), { service: 'backend' }), true);
    assert.equal(matchesFilter(rec({ service: 'backend' }), { service: 'BACKEND' }), true);
    assert.equal(matchesFilter(rec({ service: 'frontend' }), { service: 'backend' }), false);
    // Substring must NOT match service (exact only).
    assert.equal(matchesFilter(rec({ service: 'backend' }), { service: 'back' }), false);
});

test('matchesFilter: exact level (case-insensitive)', () => {
    assert.equal(matchesFilter(rec({ level: 'ERROR' }), { level: 'error' }), true);
    assert.equal(matchesFilter(rec({ level: 'info' }), { level: 'error' }), false);
});

test('matchesFilter: q is a case-insensitive substring of msg + raw', () => {
    assert.equal(matchesFilter(rec({ msg: 'spawn failed' }), { q: 'FAIL' }), true);
    assert.equal(matchesFilter(rec({ msg: 'ok' }), { q: 'fail' }), false);
    // Matches raw text too.
    assert.equal(matchesFilter(rec({ msg: '', raw: 'goroutine 42 panic' }), { q: 'panic' }), true);
    // q does NOT search attrs (mirrors logd matchLive: msg + raw only).
    assert.equal(matchesFilter(rec({ msg: 'ok', attrs: { cwd: '/workspace/x' } }), { q: 'workspace' }), false);
});

test('matchesFilter: combined predicate is AND', () => {
    const r = rec({ service: 'backend', level: 'error', msg: 'pty allocation refused' });
    assert.equal(matchesFilter(r, { service: 'backend', level: 'error', q: 'pty' }), true);
    assert.equal(matchesFilter(r, { service: 'backend', level: 'info', q: 'pty' }), false);
});

test('recordKey de-dups identical lines and separates distinct ones', () => {
    const a = rec();
    assert.equal(recordKey(a), recordKey(rec()));
    assert.notEqual(recordKey(a), recordKey(rec({ ts: '2026-07-24T10:47:12.000Z' })));
    // Raw records key on raw text.
    assert.equal(recordKey({ ts: 't', service: 's', raw: 'x' }), 't|s|x');
});

test('isRaw: raw line has raw set and no msg', () => {
    assert.equal(isRaw({ raw: 'panic', level: 'error' }), true);
    assert.equal(isRaw(rec()), false);
    assert.equal(isRaw({ msg: 'm', raw: '' }), false);
});

test('levelClass maps level → row class', () => {
    assert.equal(levelClass('error'), 'lvl-error');
    assert.equal(levelClass('DEBUG'), 'lvl-debug');
    assert.equal(levelClass('info'), 'lvl-info');
    assert.equal(levelClass(''), 'lvl-info');
});

test('fmtTime renders HH:MM:SS.mmm and tolerates junk', () => {
    assert.match(fmtTime('2026-07-24T10:47:11.164Z'), /^\d\d:\d\d:\d\d\.\d\d\d$/);
    assert.equal(fmtTime('not-a-date'), 'not-a-date');
});

test('fmtAttrs renders sorted k=v and stringifies objects', () => {
    assert.equal(fmtAttrs({ b: 2, a: 1 }), 'a=1 b=2');
    assert.equal(fmtAttrs({ o: { x: 1 } }), 'o={"x":1}');
    assert.equal(fmtAttrs(null), '');
});

test('toRow: normal record view-model', () => {
    const vm = toRow(rec({ level: 'error', msg: 'boom', attrs: { cwd: '/x' } }));
    assert.equal(vm.level, 'ERROR');
    assert.equal(vm.levelClass, 'lvl-error');
    assert.equal(vm.isError, true);
    assert.equal(vm.msg, 'boom');
    assert.equal(vm.attrs, 'cwd=/x');
});

test('toRow: raw record shows RAW level and the raw text as msg', () => {
    const vm = toRow({ ts: 't', service: 'backend', level: 'error', raw: 'goroutine 42' });
    assert.equal(vm.level, 'RAW');
    assert.equal(vm.msg, 'goroutine 42');
    assert.equal(vm.attrs, '');
    assert.equal(vm.isError, true);
});

test('toChip: up shows last-seen ago, down shows downtime', () => {
    const now = Date.parse('2026-07-24T10:47:11.000Z');
    const up = toChip({ service: 'backend', state: 'up', lastLogSeen: '2026-07-24T10:47:09.000Z' }, now);
    assert.deepEqual({ up: up.up, down: up.down }, { up: true, down: false });
    assert.equal(up.meta, '2s ago');

    const down = toChip({ service: 'backend', state: 'down', since: '2026-07-24T10:47:05.000Z' }, now);
    assert.equal(down.down, true);
    assert.equal(down.meta, 'down 6s');
});

test('toChip: up with no ingested line yet', () => {
    const chip = toChip({ service: 'frontend', state: 'up', lastLogSeen: null }, Date.now());
    assert.equal(chip.meta, 'no logs yet');
});

test('orderChips: drops logd and orders by STATUS_ORDER', () => {
    const input = [
        { service: 'holesail', state: 'up' },
        { service: 'logd', state: 'up' },
        { service: 'backend', state: 'up' },
        { service: 'sessiond', state: 'up' },
        { service: 'frontend', state: 'up' },
    ];
    const out = orderChips(input).map((s) => s.service);
    assert.deepEqual(out, STATUS_ORDER); // sessiond, backend, frontend, holesail; logd excluded
});
