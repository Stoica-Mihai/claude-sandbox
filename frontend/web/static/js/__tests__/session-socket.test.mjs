import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

const { SessionSocket } = await import('../session-socket.js');

// Fake WebSocket capturing instances so tests drive open/close/message by hand.
let sockets;
class FakeWS {
    static CONNECTING = 0; static OPEN = 1; static CLOSING = 2; static CLOSED = 3;
    constructor(url) {
        this.url = url;
        this.readyState = FakeWS.CONNECTING;
        this.sent = [];
        sockets.push(this);
    }
    send(data) { this.sent.push(data); }
    close() {
        this.readyState = FakeWS.CLOSED;
        this.onclose?.({ code: 1005 });
    }
    open() {
        this.readyState = FakeWS.OPEN;
        this.onopen?.();
    }
    drop(code) {
        this.readyState = FakeWS.CLOSED;
        this.onclose?.({ code });
    }
}

// Controllable timers: capture scheduled callbacks + delays, fire on demand.
let timers;
function makeEnv() {
    sockets = [];
    timers = [];
    globalThis.WebSocket = FakeWS;
    globalThis.location = { protocol: 'http:', host: 'test.local' };
    // Mirror layout.html's injection of the shared WS control vocabulary.
    globalThis.window = { WS_CONTROL: { resize: 'resize', deactivated: 'deactivated', reactivate: 'reactivate', error: 'error' } };
    globalThis.setTimeout = (fn, delay) => { timers.push({ fn, delay }); return timers.length; };
    globalThis.clearTimeout = (id) => { if (timers[id - 1]) timers[id - 1].cleared = true; };
}
function fireTimer(i = 0) {
    const t = timers.splice(i, 1)[0];
    t.fn();
}

let statuses;
function newSocket(callbacks = {}) {
    statuses = [];
    const sock = new SessionSocket('t1', {
        onStatus: (s, info) => statuses.push({ s, ...info }),
        ...callbacks,
    });
    return sock;
}

beforeEach(makeEnv);

test('connect → open reports open (not resumed) and enables send', () => {
    const sock = newSocket();
    sock.connect();
    assert.equal(sock.status, 'connecting');
    assert.equal(sock.send(new Uint8Array([1])), false, 'no send before open');

    sockets[0].open();
    assert.equal(sock.status, 'open');
    assert.deepEqual(statuses, [{ s: 'open', resumed: false }]);

    assert.equal(sock.send(new Uint8Array([1])), true);
    assert.equal(sock.sendResize(80, 24), true);
    assert.equal(sockets[0].sent.length, 2);
    assert.equal(sockets[0].sent[1], '{"type":"resize","cols":80,"rows":24}');
});

test('sendControl sends JSON when open, is dropped when not open', () => {
    const sock = newSocket();
    sock.connect();
    assert.equal(sock.sendControl({ type: 'reactivate' }), false, 'no control before open');

    sockets[0].open();
    assert.equal(sock.sendControl({ type: 'reactivate' }), true);
    assert.equal(sockets[0].sent.at(-1), '{"type":"reactivate"}');
});

test('normal closure (1000) → ended, no reconnect scheduled', () => {
    const sock = newSocket();
    sock.connect();
    sockets[0].open();

    sockets[0].drop(1000);

    assert.equal(sock.status, 'ended');
    assert.equal(timers.length, 0, 'no reconnect timer');
    assert.equal(sock.send(new Uint8Array([1])), false);
});

test('abnormal closure schedules reconnect with doubling backoff', () => {
    const sock = newSocket();
    sock.connect();
    sockets[0].open();

    sockets[0].drop(1006);
    assert.equal(sock.status, 'reconnecting');
    assert.deepEqual(statuses.at(-1), { s: 'reconnecting', attempt: 1, delay: 250 });
    assert.equal(timers[0].delay, 250);

    fireTimer();
    assert.equal(sockets.length, 2, 'second socket created');
    sockets[1].drop(1006);
    assert.equal(statuses.at(-1).delay, 500, 'backoff doubled');
});

test('reopen after reconnect reports resumed and resets the attempt count', () => {
    const sock = newSocket();
    sock.connect();
    sockets[0].open();
    sockets[0].drop(1006);
    fireTimer();

    sockets[1].open();
    assert.deepEqual(statuses.at(-1), { s: 'open', resumed: true });

    // Next drop starts backoff from attempt 1 again.
    sockets[1].drop(1006);
    assert.equal(statuses.at(-1).attempt, 1);
});

test('exhausted attempts → lost; retry() re-arms from scratch', () => {
    const sock = newSocket();
    sock.connect();
    for (let i = 0; i < 10; i++) {
        sockets.at(-1).drop(1006);
        fireTimer();
    }
    sockets.at(-1).drop(1006);

    assert.equal(sock.status, 'lost');
    assert.equal(timers.length, 0, 'no timer pending in lost');

    sock.retry();
    assert.equal(sock.status, 'connecting', 'retry resets the attempt count');
    sockets.at(-1).open();
    assert.equal(sock.status, 'open');
});

test('retry() announces the manual attempt via onStatus', () => {
    const sock = newSocket();
    sock.connect();
    for (let i = 0; i < 10; i++) {
        sockets.at(-1).drop(1006);
        fireTimer();
    }
    sockets.at(-1).drop(1006);
    assert.equal(sock.status, 'lost');

    sock.retry();
    const last = statuses.at(-1);
    assert.equal(last.s, 'connecting');
    assert.equal(last.retry, true, 'manual retry must be announced for UI feedback');
});

test('retry() is a no-op unless lost', () => {
    const sock = newSocket();
    sock.connect();
    sockets[0].open();
    const count = sockets.length;
    sock.retry();
    assert.equal(sockets.length, count, 'no new socket from open state');
});

test('close() is terminal: no reconnect on the 1005 it triggers', () => {
    const sock = newSocket();
    sock.connect();
    sockets[0].open();

    sock.close();

    assert.equal(sock.status, 'closed');
    assert.equal(timers.length, 0, 'no reconnect timer');
    assert.equal(statuses.filter(x => x.s === 'reconnecting').length, 0);
});

test('close() during reconnect wait cancels the pending timer', () => {
    const sock = newSocket();
    sock.connect();
    sockets[0].open();
    sockets[0].drop(1006);
    assert.equal(timers.length, 1);

    sock.close();
    assert.equal(timers[0].cleared, true, 'pending reconnect cancelled');
});

test('binary frames route to onData, control JSON to onControl, bad text to onData', () => {
    const data = [];
    const controls = [];
    const sock = newSocket({
        onData: (d) => data.push(d),
        onControl: (m) => controls.push(m),
    });
    sock.connect();
    sockets[0].open();

    const buf = new Uint8Array([104, 105]).buffer;
    sockets[0].onmessage({ data: buf });
    sockets[0].onmessage({ data: '{"type":"deactivated"}' });
    sockets[0].onmessage({ data: 'not json' });

    assert.equal(data.length, 2);
    assert.ok(data[0] instanceof Uint8Array);
    assert.equal(data[1], 'not json');
    assert.deepEqual(controls, [{ type: 'deactivated' }]);
});
