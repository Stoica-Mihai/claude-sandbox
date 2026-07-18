// Mobile touch-scroll with momentum. xterm.js v6 replaced native viewport scroll
// with a programmatic ScrollableElement, so touch swipes no longer scroll; we
// drive its pixel-level setScrollPosition directly for smooth sub-line scrolling.

const MOMENTUM_FRICTION = 0.96;
const MOMENTUM_MIN_VELOCITY = 0.5;
const MOMENTUM_MS_PER_FRAME = 16;
const MAX_TOUCH_SAMPLES = 5;

// Wire touch-drag + inertial coast scrolling onto a terminal's container.
export function wireTouchScroll(containerEl, term) {
    const viewport = containerEl.querySelector('.xterm-viewport');
    if (viewport) {
        viewport.addEventListener('click', () => term.focus());
    }
    const getSE = () => term._core?._viewport?._scrollableElement;
    const reducedMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
    let lastTouchY = 0;
    let momentumRaf = 0;
    const samples = [];

    containerEl.addEventListener('touchstart', (e) => {
        if (e.touches.length === 1) {
            cancelAnimationFrame(momentumRaf);
            lastTouchY = e.touches[0].clientY;
            samples.length = 0;
        }
    }, { passive: true });

    containerEl.addEventListener('touchmove', (e) => {
        if (e.touches.length !== 1) return;
        const currentY = e.touches[0].clientY;
        const dy = lastTouchY - currentY;
        lastTouchY = currentY;
        samples.push({ dy, t: performance.now() });
        if (samples.length > MAX_TOUCH_SAMPLES) samples.shift();
        const se = getSE();
        if (!se) return;
        const pos = se.getScrollPosition();
        se.setScrollPosition({ scrollTop: pos.scrollTop + dy });
    }, { passive: true });

    containerEl.addEventListener('touchend', () => {
        if (reducedMotion) return; // scroll stops with the finger, no inertia coast
        if (samples.length < 2) return;
        const se = getSE();
        if (!se) return;
        const first = samples[0];
        const last = samples[samples.length - 1];
        const dt = last.t - first.t;
        if (dt <= 0) return;
        const totalDy = samples.reduce((sum, s) => sum + s.dy, 0);
        let vel = (totalDy / dt) * MOMENTUM_MS_PER_FRAME;

        const coast = () => {
            vel *= MOMENTUM_FRICTION;
            if (Math.abs(vel) < MOMENTUM_MIN_VELOCITY) return;
            const pos = se.getScrollPosition();
            se.setScrollPosition({ scrollTop: pos.scrollTop + vel });
            momentumRaf = requestAnimationFrame(coast);
        };
        momentumRaf = requestAnimationFrame(coast);
    }, { passive: true });
}
