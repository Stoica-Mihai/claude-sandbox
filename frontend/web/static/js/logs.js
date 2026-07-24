// Logs surface manager: owns the /logs view — status strip, filter bar, the
// dense log table, live-tail, and scroll-load history. Mirrors the
// Terminal/Chat manager shape (create/destroy/get) so main.js can boot it per
// route. There is a single logs view, so the manager holds one instance.
//
// Data comes from the logd sidecar via the frontend's guarded proxy:
//   GET /api/logs?service&level&q&until&limit   — query + scroll-load older
//   GET /api/logs/stream?service&level&q         — SSE live-tail
//   GET /api/status  + /api/status/stream        — status strip
// Filters mirror the query API exactly (logs-events.matchesFilter).

import { createStickyScroll } from './chat-scroll.js';
import { matchesFilter, recordKey } from './logs-events.js';
import { appendRow, prependRow, clearRows, showNote, renderStatus } from './logs-render.js';
import { logsPath, logsStreamPath, statusPath, statusStreamPath } from './routes.js';

const PAGE_LIMIT = 300;          // records per query / scroll-load page
const LOAD_OLDER_PX = 240;       // scroll-from-top distance that triggers a load
const STATUS_TICK_MS = 2000;     // re-render the strip so relative times stay fresh
const SEARCH_DEBOUNCE_MS = 200;

export const LogsManager = {
    instance: null,

    // create wires the (server-rendered) logs view. containerEl is the .main
    // element on the /logs page; the view markup is already inside it.
    create(containerEl) {
        if (this.instance) this.destroy();

        const q = (sel) => containerEl.querySelector(sel);
        const strip = q('.lz-status');
        const list = q('.lz-list');
        const flow = q('.lz-flow');
        const search = q('.lz-filters input[type=search]');
        const tailToggle = q('.lz-tail .toggle');
        const jump = q('.lz-jump');
        const liveInd = q('.lz-foot .lz-live');
        const liveLabel = q('.lz-live-label');
        const countFilter = q('.lz-filters .lz-count');
        const countFoot = q('.lz-foot .lz-count');
        if (!list || !flow) return null;

        const inst = {
            containerEl, strip, list, flow, search, tailToggle, jump,
            liveInd, liveLabel, countFilter, countFoot,
            filter: { service: 'all', level: 'all', q: '' },
            live: true,
            records: [], seen: new Set(),
            oldestTs: null, hasMoreOlder: true, loadingOlder: false,
            unavailable: false,
            logStream: null, statusStream: null,
            lastStatuses: [], statusTimer: null, searchTimer: null,
            sticky: null, destroyed: false,
        };
        this.instance = inst;

        inst.sticky = createStickyScroll(list, flow, {
            onFollowChange: () => this._updateLive(inst),
        });

        this._wireControls(inst);

        this._reload(inst);
        this._connectStatus(inst);
        inst.statusTimer = setInterval(() => this._renderStatus(inst), STATUS_TICK_MS);

        return inst;
    },

    _wireControls(inst) {
        // Segmented service/level filters.
        inst.containerEl.querySelectorAll('.seg[data-kind]').forEach((seg) => {
            const kind = seg.getAttribute('data-kind');
            seg.querySelectorAll('button').forEach((b) => {
                b.addEventListener('click', () => {
                    seg.querySelectorAll('button').forEach((x) => {
                        x.classList.remove('on');
                        x.setAttribute('aria-pressed', 'false');
                    });
                    b.classList.add('on');
                    b.setAttribute('aria-pressed', 'true');
                    inst.filter[kind] = b.dataset.val;
                    this._reload(inst);
                });
            });
        });

        if (inst.search) {
            inst.search.addEventListener('input', () => {
                clearTimeout(inst.searchTimer);
                inst.searchTimer = setTimeout(() => {
                    inst.filter.q = inst.search.value.trim();
                    this._reload(inst);
                }, SEARCH_DEBOUNCE_MS);
            });
        }

        if (inst.tailToggle) {
            const flip = () => this._setLive(inst, !inst.live);
            inst.tailToggle.addEventListener('click', flip);
            inst.tailToggle.addEventListener('keydown', (e) => {
                if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); flip(); }
            });
        }

        if (inst.jump) inst.jump.addEventListener('click', () => inst.sticky.engage());

        inst.list.addEventListener('scroll', () => {
            if (inst.list.scrollTop <= LOAD_OLDER_PX) this._loadOlder(inst);
        });
    },

    // _query builds the shared filter query string (service/level/q).
    _query(inst) {
        const p = new URLSearchParams();
        if (inst.filter.service && inst.filter.service !== 'all') p.set('service', inst.filter.service);
        if (inst.filter.level && inst.filter.level !== 'all') p.set('level', inst.filter.level);
        if (inst.filter.q) p.set('q', inst.filter.q);
        return p;
    },

    // _reload clears the buffer and fetches the newest page for the current
    // filters, then (re)connects the live stream. Called on open + filter change.
    async _reload(inst) {
        inst.records = [];
        inst.seen = new Set();
        inst.oldestTs = null;
        inst.hasMoreOlder = true;
        inst.unavailable = false;
        clearRows(inst.flow);

        const p = this._query(inst);
        p.set('limit', String(PAGE_LIMIT));
        let recs;
        try {
            const res = await fetch(logsPath() + '?' + p.toString());
            if (!res.ok) throw new Error('logd ' + res.status);
            recs = await res.json();
        } catch (e) {
            if (inst.destroyed) return;
            inst.unavailable = true;
            showNote(inst.flow, 'Log service unavailable.', true);
            this._updateCounts(inst);
            this._connectLog(inst); // stream may still recover
            return;
        }
        if (inst.destroyed) return;

        // Query returns newest-first; render chronological (oldest→newest, bottom = newest).
        const chrono = (Array.isArray(recs) ? recs : []).slice().reverse();
        for (const rec of chrono) {
            const k = recordKey(rec);
            if (inst.seen.has(k)) continue;
            inst.seen.add(k);
            inst.records.push(rec);
            appendRow(inst.flow, rec);
        }
        inst.oldestTs = inst.records.length ? inst.records[0].ts : null;
        if (!inst.records.length) showNote(inst.flow, 'No log lines match.', false);
        this._updateCounts(inst);
        inst.sticky.engage();

        this._connectLog(inst);
    },

    _connectLog(inst) {
        if (inst.logStream) { inst.logStream.close(); inst.logStream = null; }
        if (!inst.live || typeof EventSource === 'undefined') { this._updateLive(inst); return; }
        const es = new EventSource(logsStreamPath() + '?' + this._query(inst).toString());
        es.onmessage = (ev) => {
            if (inst.destroyed) return;
            let rec;
            try { rec = JSON.parse(ev.data); } catch (e) { return; }
            const k = recordKey(rec);
            if (inst.seen.has(k)) return;
            if (!matchesFilter(rec, inst.filter)) return; // guard (server already filters)
            if (inst.unavailable) { inst.unavailable = false; clearRows(inst.flow); }
            inst.seen.add(k);
            inst.records.push(rec);
            if (!inst.oldestTs) inst.oldestTs = rec.ts;
            appendRow(inst.flow, rec);
            this._updateCounts(inst);
        };
        es.onerror = () => { /* EventSource auto-reconnects; the query fetch owns the unavailable note */ };
        inst.logStream = es;
        this._updateLive(inst);
    },

    // _loadOlder fetches the page just older than the oldest shown line and
    // prepends it (no pager; scroll-up history).
    async _loadOlder(inst) {
        if (inst.loadingOlder || !inst.hasMoreOlder || !inst.oldestTs || inst.unavailable) return;
        inst.loadingOlder = true;
        const p = this._query(inst);
        p.set('until', inst.oldestTs);
        p.set('limit', String(PAGE_LIMIT));
        let recs;
        try {
            const res = await fetch(logsPath() + '?' + p.toString());
            if (!res.ok) throw new Error('logd ' + res.status);
            recs = await res.json();
        } catch (e) {
            inst.hasMoreOlder = false;
            inst.loadingOlder = false;
            return;
        }
        if (inst.destroyed) { inst.loadingOlder = false; return; }

        // Newest-first; prepend newest→oldest so the top ends up oldest.
        const arr = Array.isArray(recs) ? recs : [];
        let added = 0;
        inst.sticky.disengage(); // reading history — don't snap back to the bottom
        for (const rec of arr) {
            const k = recordKey(rec);
            if (inst.seen.has(k)) continue;
            inst.seen.add(k);
            inst.records.unshift(rec);
            prependRow(inst.flow, rec);
            added++;
        }
        if (added) inst.oldestTs = inst.records[0].ts;
        if (added === 0 || arr.length < PAGE_LIMIT) inst.hasMoreOlder = false;
        this._updateCounts(inst);
        inst.loadingOlder = false;
    },

    async _connectStatus(inst) {
        try {
            const res = await fetch(statusPath());
            if (res.ok) { inst.lastStatuses = await res.json(); this._renderStatus(inst); }
        } catch (e) { /* strip stays empty; logd unavailable */ }
        if (inst.destroyed || typeof EventSource === 'undefined') return;
        const es = new EventSource(statusStreamPath());
        es.onmessage = (ev) => {
            if (inst.destroyed) return;
            try { inst.lastStatuses = JSON.parse(ev.data); } catch (e) { return; }
            this._renderStatus(inst);
        };
        es.onerror = () => { /* auto-reconnects */ };
        inst.statusStream = es;
    },

    _renderStatus(inst) {
        if (inst.strip && inst.lastStatuses.length) renderStatus(inst.strip, inst.lastStatuses, Date.now());
    },

    _setLive(inst, on) {
        inst.live = on;
        if (inst.tailToggle) {
            inst.tailToggle.classList.toggle('on', on);
            inst.tailToggle.setAttribute('aria-checked', on ? 'true' : 'false');
        }
        if (on) { this._connectLog(inst); inst.sticky.engage(); }
        else if (inst.logStream) { inst.logStream.close(); inst.logStream = null; }
        this._updateLive(inst);
    },

    // _updateLive reflects the live/paused footer indicator + jump pill.
    _updateLive(inst) {
        const following = inst.sticky ? inst.sticky.isFollowing() : true;
        const live = inst.live && following;
        if (inst.liveInd) inst.liveInd.classList.toggle('paused', !live);
        if (inst.liveLabel) inst.liveLabel.textContent = live ? 'live' : 'paused';
    },

    _updateCounts(inst) {
        const n = inst.records.length;
        const lines = n + ' line' + (n === 1 ? '' : 's');
        if (inst.countFilter) inst.countFilter.textContent = lines;
        if (inst.countFoot) inst.countFoot.textContent = lines + this._span(inst);
    },

    // _span renders " · last Nm" from the oldest shown line to now.
    _span(inst) {
        if (!inst.records.length) return '';
        const oldest = new Date(inst.records[0].ts).getTime();
        if (isNaN(oldest)) return '';
        const s = Math.max(0, Math.round((Date.now() - oldest) / 1000));
        if (s < 60) return ' · last ' + s + 's';
        const m = Math.floor(s / 60);
        if (m < 60) return ' · last ' + m + 'm';
        return ' · last ' + Math.floor(m / 60) + 'h';
    },

    destroy() {
        const inst = this.instance;
        if (!inst) return;
        inst.destroyed = true;
        clearTimeout(inst.searchTimer);
        clearInterval(inst.statusTimer);
        inst.logStream?.close();
        inst.statusStream?.close();
        inst.sticky?.destroy();
        this.instance = null;
    },

    get() { return this.instance; },
};

// init boots the logs view when the /logs page is served (its markup is
// present). No import-time side effects; resets the singleton for tests.
export function init() {
    LogsManager.instance = null;
    if (typeof document === 'undefined') return;
    const main = document.querySelector('.main--logs');
    if (main && main.querySelector('.lz-list')) LogsManager.create(main);
}
