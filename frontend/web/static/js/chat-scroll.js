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

export function createStickyScroll(scrollEl, contentEl) {
    let follow = true;
    let pendingProgrammatic = 0;

    const pin = () => {
        const target = scrollEl.scrollHeight - scrollEl.clientHeight;
        if (scrollEl.scrollTop === target) return;
        pendingProgrammatic++;
        scrollEl.scrollTop = scrollEl.scrollHeight;
    };

    const onScroll = () => {
        if (pendingProgrammatic > 0) {
            pendingProgrammatic--;
            return;
        }
        follow = scrollEl.scrollHeight - scrollEl.scrollTop - scrollEl.clientHeight < FOLLOW_THRESHOLD;
    };
    scrollEl.addEventListener('scroll', onScroll);

    let observer = null;
    if (typeof ResizeObserver !== 'undefined') {
        observer = new ResizeObserver(() => {
            if (follow) pin();
        });
        observer.observe(contentEl);
        observer.observe(scrollEl);
    }

    return {
        // engage: user intent to be at the bottom (sending a message).
        engage() {
            follow = true;
            pin();
        },
        // disengage: user intent to read away from the bottom (load earlier).
        disengage() {
            follow = false;
        },
        isFollowing() {
            return follow;
        },
        destroy() {
            scrollEl.removeEventListener('scroll', onScroll);
            observer?.disconnect();
        },
    };
}
