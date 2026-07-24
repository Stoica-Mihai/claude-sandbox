// Sticky bottom-scroll policy for the chat list. Single owner of all scroll
// decisions: the renderer never scrolls, and nothing is timing-based.
//
// Mechanism: a ResizeObserver on the content element re-pins the scroll
// whenever content height changes — streaming growth, markdown re-render,
// font/emoji swap, image load — and one on the scroll container covers
// viewport changes (mobile keyboard, window resize). Follow state comes from
// the user's own scroll events; programmatic scrolls are discriminated with
// a pending counter so they never disengage the follow.

const FOLLOW_THRESHOLD = 120; // px from bottom within which scrolling re-engages

// StickyScroll owns the follow state + observers for one chat list. Its listener
// and observer callbacks are bound arrow fields so they attach/detach cleanly.
export class StickyScroll {
    constructor(scrollEl, contentEl, opts = {}) {
        this.scrollEl = scrollEl;
        this.contentEl = contentEl;
        this.opts = opts;
        this.follow = true;
        this.pendingProgrammatic = 0;
        // User-initiated expansion (tool/thinking toggles) grows content but is
        // not output — following it would yank the opened body past the
        // viewport. Suppression is a lazily-checked window, not a timer.
        this.suppressUntil = 0;
        this.observer = null;

        scrollEl.addEventListener('scroll', this._onScroll);
        if (typeof ResizeObserver !== 'undefined') {
            this.observer = new ResizeObserver(this._onResize);
            this.observer.observe(contentEl);
            this.observer.observe(scrollEl);
        }
    }

    // _setFollow notifies onFollowChange only on an actual edge — drives the
    // jump-to-latest button (shown while NOT following).
    _setFollow(v) {
        if (v === this.follow) return;
        this.follow = v;
        this.opts.onFollowChange?.(this.follow);
    }

    _pin() {
        const target = this.scrollEl.scrollHeight - this.scrollEl.clientHeight;
        if (this.scrollEl.scrollTop === target) return;
        this.pendingProgrammatic++;
        this.scrollEl.scrollTop = this.scrollEl.scrollHeight;
    }

    _onScroll = () => {
        if (this.pendingProgrammatic > 0) {
            this.pendingProgrammatic--;
            return;
        }
        this._setFollow(this.scrollEl.scrollHeight - this.scrollEl.scrollTop - this.scrollEl.clientHeight < FOLLOW_THRESHOLD);
    };

    _onResize = () => {
        // One-shot: the toggle's layout burst is a single callback, so consume
        // the suppression immediately — streamed growth arriving right after a
        // toggle still pins instead of being eaten by a wall-clock window.
        if (Date.now() < this.suppressUntil) {
            this.suppressUntil = 0;
            return;
        }
        if (this.follow) this._pin();
    };

    // engage: user intent to be at the bottom (sending a message, or the
    // jump-to-latest button).
    engage() {
        this._setFollow(true);
        this._pin();
    }

    // disengage: user intent to read away from the bottom (load earlier).
    disengage() {
        this._setFollow(false);
    }

    // suppressNext: ignore resize-driven pins for the burst caused by a
    // user-initiated layout change (expand/collapse toggles).
    suppressNext() {
        this.suppressUntil = Date.now() + 200;
    }

    isFollowing() {
        return this.follow;
    }

    destroy() {
        this.scrollEl.removeEventListener('scroll', this._onScroll);
        this.observer?.disconnect();
    }
}

export function createStickyScroll(scrollEl, contentEl, opts = {}) {
    return new StickyScroll(scrollEl, contentEl, opts);
}
