// Logs surface: the /logs view — status strip, filter bar, the dense log table,
// live-tail, and scroll-load history. LogsView is the per-view class (DOM refs,
// buffer, filters, streams); LogsManager is the factory + registry that holds
// the single view and exposes create/get/destroy (surface.js drives it per
// route switch). Stateless work stays in the pure helpers (logs-events,
// logs-render).
//
// Ordering: NEWEST-FIRST, newest at the TOP (the query API's order). Live-tail
// prepends new lines at the top; scrolling toward the bottom lazily loads older
// history; the jump pill returns to the newest (top).
//
// Data comes from the logd sidecar via the frontend's guarded proxy:
//   GET /api/logs?service&level&q&until&limit   — query + scroll-load older
//   GET /api/logs/stream?service&level&q         — SSE live-tail
//   GET /api/status  + /api/status/stream        — status strip
// Filters mirror the query API exactly (logs-events.matchesFilter).

import { matchesFilter, recordKey } from './logs-events.js';
import { appendRow, prependRow, clearRows, showNote, renderStatus } from './logs-render.js';
import { logsPath, logsStreamPath, statusPath, statusStreamPath } from './routes.js';

const PAGE_LIMIT = 300;          // records per query / scroll-load page
const LOAD_OLDER_PX = 240;       // scroll-from-bottom distance that triggers a load
const FOLLOW_PX = 40;            // within this of the top = "following" the newest
const STATUS_TICK_MS = 2000;     // re-render the strip so relative times stay fresh
const SEARCH_DEBOUNCE_MS = 200;

// LogsView owns one rendered logs surface. Constructed from server-rendered
// markup (containerEl); start() wires controls + loads the first page + connects
// the streams. Guarded by this.destroyed so an in-flight fetch/stream can't
// touch a torn-down view.
export class LogsView {
    constructor(containerEl) {
        const q = (sel) => containerEl.querySelector(sel);
        this.containerEl = containerEl;
        this.strip = q('.lz-status');
        this.list = q('.lz-list');
        this.flow = q('.lz-flow');
        this.search = q('.lz-filters input[type=search]');
        this.tailToggle = q('.lz-tail .toggle');
        this.jump = q('.lz-jump');
        this.liveInd = q('.lz-foot .lz-live');
        this.liveLabel = q('.lz-live-label');
        this.countFilter = q('.lz-filters .lz-count');
        this.countFoot = q('.lz-foot .lz-count');

        this.filter = { service: 'all', level: 'all', q: '' };
        this.live = true;
        this.records = [];
        this.seen = new Set();
        this.oldestTs = null;
        this.hasMoreOlder = true;
        this.loadingOlder = false;
        this.unavailable = false;
        this.logStream = null;
        this.statusStream = null;
        this.lastStatuses = [];
        this.statusTimer = null;
        this.searchTimer = null;
        this.destroyed = false;
    }

    start() {
        this.wireControls();
        this.reload();
        this.connectStatus();
        this.statusTimer = setInterval(() => this.renderStatus(), STATUS_TICK_MS);
    }

    // following reports whether the view is pinned to the newest (top) line.
    following() {
        return this.list.scrollTop <= FOLLOW_PX;
    }

    wireControls() {
        // Segmented service/level filters.
        this.containerEl.querySelectorAll('.seg[data-kind]').forEach((seg) => {
            const kind = seg.getAttribute('data-kind');
            seg.querySelectorAll('button').forEach((b) => {
                b.addEventListener('click', () => {
                    seg.querySelectorAll('button').forEach((x) => {
                        x.classList.remove('on');
                        x.setAttribute('aria-pressed', 'false');
                    });
                    b.classList.add('on');
                    b.setAttribute('aria-pressed', 'true');
                    this.filter[kind] = b.dataset.val;
                    this.reload();
                });
            });
        });

        if (this.search) {
            this.search.addEventListener('input', () => {
                clearTimeout(this.searchTimer);
                this.searchTimer = setTimeout(() => {
                    this.filter.q = this.search.value.trim();
                    this.reload();
                }, SEARCH_DEBOUNCE_MS);
            });
        }

        if (this.tailToggle) {
            const flip = () => this.setLive(!this.live);
            this.tailToggle.addEventListener('click', flip);
            this.tailToggle.addEventListener('keydown', (e) => {
                if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); flip(); }
            });
        }

        // Jump pill returns to the newest line (top).
        if (this.jump) this.jump.addEventListener('click', () => { this.list.scrollTop = 0; this.updateLive(); });

        this.list.addEventListener('scroll', () => {
            // Older lines live toward the bottom (newest-first), so load near the bottom.
            if (this.list.scrollHeight - this.list.scrollTop - this.list.clientHeight <= LOAD_OLDER_PX) {
                this.loadOlder();
            }
            this.updateLive();
        });
    }

    // query builds the shared filter query string (service/level/q).
    query() {
        const p = new URLSearchParams();
        if (this.filter.service && this.filter.service !== 'all') p.set('service', this.filter.service);
        if (this.filter.level && this.filter.level !== 'all') p.set('level', this.filter.level);
        if (this.filter.q) p.set('q', this.filter.q);
        return p;
    }

    // reload clears the buffer and fetches the newest page for the current
    // filters, then (re)connects the live stream. Called on open + filter change.
    async reload() {
        this.records = [];
        this.seen = new Set();
        this.oldestTs = null;
        this.hasMoreOlder = true;
        this.unavailable = false;
        clearRows(this.flow);

        const p = this.query();
        p.set('limit', String(PAGE_LIMIT));
        let recs;
        try {
            const res = await fetch(logsPath() + '?' + p.toString());
            if (!res.ok) throw new Error('logd ' + res.status);
            recs = await res.json();
        } catch (e) {
            if (this.destroyed) return;
            this.unavailable = true;
            showNote(this.flow, 'Log service unavailable.', true);
            this.updateCounts();
            this.connectLog(); // stream may still recover
            return;
        }
        if (this.destroyed) return;

        // Query returns newest-first; render as-is → newest at top, oldest at bottom.
        const arr = Array.isArray(recs) ? recs : [];
        for (const rec of arr) {
            const k = recordKey(rec);
            if (this.seen.has(k)) continue;
            this.seen.add(k);
            this.records.push(rec);
            appendRow(this.flow, rec);
        }
        this.oldestTs = this.records.length ? this.records[this.records.length - 1].ts : null;
        if (!this.records.length) showNote(this.flow, 'No log lines match.', false);
        this.list.scrollTop = 0; // pin to the newest (top)
        this.updateCounts();
        this.updateLive();

        this.connectLog();
    }

    connectLog() {
        if (this.logStream) { this.logStream.close(); this.logStream = null; }
        if (!this.live || typeof EventSource === 'undefined') { this.updateLive(); return; }
        const es = new EventSource(logsStreamPath() + '?' + this.query().toString());
        es.onmessage = (ev) => {
            if (this.destroyed) return;
            let rec;
            try { rec = JSON.parse(ev.data); } catch (e) { return; }
            const k = recordKey(rec);
            if (this.seen.has(k)) return;
            if (!matchesFilter(rec, this.filter)) return; // guard (server already filters)
            if (this.unavailable) { this.unavailable = false; clearRows(this.flow); }
            this.seen.add(k);
            this.records.unshift(rec); // newest to the front

            // Prepend at the top. If the user is following (at the top), keep the
            // newest in view; otherwise anchor their scroll so the inserted row
            // above doesn't shift what they're reading.
            const following = this.following();
            const before = this.list.scrollHeight;
            prependRow(this.flow, rec);
            if (following) this.list.scrollTop = 0;
            else this.list.scrollTop += this.list.scrollHeight - before;

            this.updateCounts();
            this.updateLive();
        };
        es.onerror = () => { /* EventSource auto-reconnects; the query fetch owns the unavailable note */ };
        this.logStream = es;
        this.updateLive();
    }

    // loadOlder fetches the page just older than the oldest shown line and
    // appends it at the bottom (no pager; scroll-down history).
    async loadOlder() {
        if (this.loadingOlder || !this.hasMoreOlder || !this.oldestTs || this.unavailable) return;
        this.loadingOlder = true;
        const p = this.query();
        p.set('until', this.oldestTs);
        p.set('limit', String(PAGE_LIMIT));
        let recs;
        try {
            const res = await fetch(logsPath() + '?' + p.toString());
            if (!res.ok) throw new Error('logd ' + res.status);
            recs = await res.json();
        } catch (e) {
            this.hasMoreOlder = false;
            this.loadingOlder = false;
            return;
        }
        if (this.destroyed) { this.loadingOlder = false; return; }

        // Newest-first, all older than oldestTs; append newest→oldest at the
        // bottom so the list stays newest-first end to end.
        const arr = Array.isArray(recs) ? recs : [];
        let added = 0;
        for (const rec of arr) {
            const k = recordKey(rec);
            if (this.seen.has(k)) continue;
            this.seen.add(k);
            this.records.push(rec);
            appendRow(this.flow, rec);
            added++;
        }
        if (added) this.oldestTs = this.records[this.records.length - 1].ts;
        if (added === 0 || arr.length < PAGE_LIMIT) this.hasMoreOlder = false;
        this.updateCounts();
        this.loadingOlder = false;
    }

    async connectStatus() {
        try {
            const res = await fetch(statusPath());
            if (res.ok) { this.lastStatuses = await res.json(); this.renderStatus(); }
        } catch (e) { /* strip stays empty; logd unavailable */ }
        if (this.destroyed || typeof EventSource === 'undefined') return;
        const es = new EventSource(statusStreamPath());
        es.onmessage = (ev) => {
            if (this.destroyed) return;
            try { this.lastStatuses = JSON.parse(ev.data); } catch (e) { return; }
            this.renderStatus();
        };
        es.onerror = () => { /* auto-reconnects */ };
        this.statusStream = es;
    }

    renderStatus() {
        if (this.strip && this.lastStatuses.length) renderStatus(this.strip, this.lastStatuses, Date.now());
    }

    setLive(on) {
        this.live = on;
        if (this.tailToggle) {
            this.tailToggle.classList.toggle('on', on);
            this.tailToggle.setAttribute('aria-checked', on ? 'true' : 'false');
        }
        if (on) { this.connectLog(); this.list.scrollTop = 0; } // resume → jump to newest
        else if (this.logStream) { this.logStream.close(); this.logStream = null; }
        this.updateLive();
    }

    // updateLive reflects the live/paused footer indicator + the jump pill,
    // which shows only when scrolled away from the newest (top).
    updateLive() {
        const following = this.following();
        const live = this.live && following;
        if (this.liveInd) this.liveInd.classList.toggle('paused', !live);
        if (this.liveLabel) this.liveLabel.textContent = live ? 'live' : 'paused';
        if (this.jump) this.jump.classList.toggle('hidden', following);
    }

    updateCounts() {
        const n = this.records.length;
        const lines = n + ' line' + (n === 1 ? '' : 's');
        if (this.countFilter) this.countFilter.textContent = lines;
        if (this.countFoot) this.countFoot.textContent = lines + this.span();
    }

    // span renders " · last Nm" from the oldest shown line (bottom) to now.
    span() {
        if (!this.records.length) return '';
        const oldest = new Date(this.records[this.records.length - 1].ts).getTime();
        if (isNaN(oldest)) return '';
        const s = Math.max(0, Math.round((Date.now() - oldest) / 1000));
        if (s < 60) return ' · last ' + s + 's';
        const m = Math.floor(s / 60);
        if (m < 60) return ' · last ' + m + 'm';
        return ' · last ' + Math.floor(m / 60) + 'h';
    }

    destroy() {
        this.destroyed = true;
        clearTimeout(this.searchTimer);
        clearInterval(this.statusTimer);
        this.logStream?.close();
        this.statusStream?.close();
    }
}

// LogsManager is the factory + registry for the single logs view. Callers
// (surface.js) use create/get/destroy; there is one logs view at a time.
export const LogsManager = {
    instance: null,

    // create wires a LogsView over the server-rendered markup in containerEl.
    // Returns null (and creates nothing) if the log markup is absent.
    create(containerEl) {
        if (this.instance) this.destroy();
        const view = new LogsView(containerEl);
        if (!view.list || !view.flow) return null;
        this.instance = view;
        view.start();
        return view;
    },

    destroy() {
        if (!this.instance) return;
        this.instance.destroy();
        this.instance = null;
    },

    get() { return this.instance; },
};

// init resets the singleton. surface.js owns create/destroy per view switch
// (the logs surface is one of two always-mounted surfaces), so init no longer
// auto-creates. No import-time side effects.
export function init() {
    LogsManager.instance = null;
}
