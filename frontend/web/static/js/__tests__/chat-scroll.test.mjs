// chat-scroll.js sticky-follow policy tests: geometry-faked scroll element,
// stubbed ResizeObserver, no DOM library.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { createStickyScroll } from '../chat-scroll.js';

// Minimal scroll-element fake: settable geometry + scroll listener dispatch.
function fakeScrollEl() {
    const listeners = [];
    return {
        scrollHeight: 1000,
        clientHeight: 200,
        scrollTop: 0,
        addEventListener(type, fn) { if (type === 'scroll') listeners.push(fn); },
        removeEventListener(type, fn) {
            const i = listeners.indexOf(fn);
            if (i >= 0) listeners.splice(i, 1);
        },
        fireScroll() { listeners.slice().forEach(fn => fn()); },
        listenerCount() { return listeners.length; },
    };
}

// ResizeObserver stub capturing the callback + observed targets.
class FakeResizeObserver {
    constructor(cb) { this.cb = cb; this.targets = []; FakeResizeObserver.last = this; }
    observe(t) { this.targets.push(t); }
    disconnect() { this.targets = []; this.disconnected = true; }
    fire() { this.cb([]); }
}

function withRO(fn) {
    const prev = globalThis.ResizeObserver;
    globalThis.ResizeObserver = FakeResizeObserver;
    try { return fn(); } finally { globalThis.ResizeObserver = prev; }
}

test('content growth re-pins to the bottom while following', () => {
    withRO(() => {
        const el = fakeScrollEl();
        createStickyScroll(el, {});
        el.scrollHeight = 2000;
        FakeResizeObserver.last.fire();
        assert.equal(el.scrollTop, 2000);
    });
});

test('a user scroll away from the bottom disengages the follow', () => {
    withRO(() => {
        const el = fakeScrollEl();
        createStickyScroll(el, {});
        el.scrollTop = 100; // far from bottom (1000 - 100 - 200 = 700)
        el.fireScroll();
        el.scrollHeight = 3000;
        FakeResizeObserver.last.fire();
        assert.equal(el.scrollTop, 100); // growth did not yank the position
    });
});

test('scrolling back to the bottom re-engages the follow', () => {
    withRO(() => {
        const el = fakeScrollEl();
        const sticky = createStickyScroll(el, {});
        el.scrollTop = 100;
        el.fireScroll();
        assert.equal(sticky.isFollowing(), false);
        el.scrollTop = 790; // within 120 of bottom
        el.fireScroll();
        assert.equal(sticky.isFollowing(), true);
    });
});

test('programmatic pins never disengage the follow', () => {
    withRO(() => {
        const el = fakeScrollEl();
        const sticky = createStickyScroll(el, {});
        el.scrollHeight = 5000;
        FakeResizeObserver.last.fire(); // pin -> programmatic scroll
        el.fireScroll();                // the scroll event that pin caused
        assert.equal(sticky.isFollowing(), true);
    });
});

test('engage forces the bottom from anywhere; disengage stops re-pins', () => {
    withRO(() => {
        const el = fakeScrollEl();
        const sticky = createStickyScroll(el, {});
        el.scrollTop = 0;
        el.fireScroll(); // user reading at the top
        sticky.engage(); // send
        assert.equal(el.scrollTop, 1000);
        assert.equal(sticky.isFollowing(), true);

        sticky.disengage(); // load-earlier
        el.scrollHeight = 9000;
        FakeResizeObserver.last.fire();
        assert.equal(el.scrollTop, 1000);
    });
});

test('destroy disconnects the observer and the scroll listener', () => {
    withRO(() => {
        const el = fakeScrollEl();
        const sticky = createStickyScroll(el, {});
        const ro = FakeResizeObserver.last;
        sticky.destroy();
        assert.equal(ro.disconnected, true);
        assert.equal(el.listenerCount(), 0);
    });
});

test('works without ResizeObserver (test/jsdom-less environments)', () => {
    const prev = globalThis.ResizeObserver;
    delete globalThis.ResizeObserver;
    try {
        const el = fakeScrollEl();
        const sticky = createStickyScroll(el, {});
        sticky.engage();
        assert.equal(el.scrollTop, 1000);
    } finally {
        if (prev) globalThis.ResizeObserver = prev;
    }
});

test('suppressNext skips the resize pin burst, later growth pins again', async () => {
    withRO(() => {
        const el = fakeScrollEl();
        const sticky = createStickyScroll(el, {});
        el.scrollTop = 800; // at bottom, following

        sticky.suppressNext();
        el.scrollHeight = 3000; // user expanded a tool body
        FakeResizeObserver.last.fire();
        assert.equal(el.scrollTop, 800); // no yank

        return new Promise(r => setTimeout(() => {
            el.scrollHeight = 4000; // streamed output later
            FakeResizeObserver.last.fire();
            assert.equal(el.scrollTop, 4000);
            r();
        }, 220));
    });
});
