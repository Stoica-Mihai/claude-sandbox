// Pure log-record / status translation. No DOM here — this module turns a
// LogRecord (from GET /api/logs or the SSE stream) into a render view-model,
// owns the client-side filter predicate that mirrors the query API, and turns
// a ServiceStatus into a status-chip view-model. Kept DOM-free so the mapping
// and the filter logic are unit-testable without a browser.
//
// Record shape (shared/logrecord.go): { ts, service, level, msg?, attrs?, raw? }
// Status shape (shared/servicestatus.go): { service, state, since, lastLogSeen }

// STATUS_ORDER is the fixed left-to-right chip order (matches the locked
// mockup). logd is excluded — it serves the status, it is not a probed peer.
export const STATUS_ORDER = ['sessiond', 'backend', 'frontend', 'holesail'];

// matchesFilter mirrors logd's live SSE filter (store.go matchLive): exact
// service (case-insensitive, unless "all"/empty), exact level (case-insensitive,
// unless "all"/empty), and a case-insensitive substring of msg + " " + raw.
export function matchesFilter(rec, filter) {
    if (!rec) return false;
    const f = filter || {};
    const svc = f.service && f.service !== 'all' ? f.service.toLowerCase() : '';
    const lvl = f.level && f.level !== 'all' ? f.level.toLowerCase() : '';
    const q = (f.q || '').trim().toLowerCase();
    if (svc && (rec.service || '').toLowerCase() !== svc) return false;
    if (lvl && (rec.level || '').toLowerCase() !== lvl) return false;
    if (q) {
        const hay = ((rec.msg || '') + ' ' + (rec.raw || '')).toLowerCase();
        if (!hay.includes(q)) return false;
    }
    return true;
}

// recordKey is a stable de-dup key: the SSE replay tail overlaps the initial
// query window, so a record can arrive from both paths. ts is RFC3339Nano, so
// ts+service+(raw|msg) is unique per line in practice.
export function recordKey(rec) {
    return (rec.ts || '') + '|' + (rec.service || '') + '|' + (rec.raw || rec.msg || '');
}

// isRaw reports a non-JSON line (logd sets raw + level "error", no msg).
export function isRaw(rec) {
    return !!(rec && rec.raw) && !rec.msg;
}

// levelClass maps a level to its row modifier class.
export function levelClass(level) {
    switch ((level || '').toLowerCase()) {
        case 'error': return 'lvl-error';
        case 'debug': return 'lvl-debug';
        default: return 'lvl-info';
    }
}

// fmtTime renders an ISO timestamp as HH:MM:SS.mmm (local), the mockup's dense
// column. Falls back to the raw string when unparseable.
export function fmtTime(ts) {
    const d = new Date(ts);
    if (isNaN(d.getTime())) return String(ts || '');
    const p = (n, w = 2) => String(n).padStart(w, '0');
    return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds()) + '.' + p(d.getMilliseconds(), 3);
}

// fmtAttrs renders a record's attrs map as "k=v k=v" (the muted trailing text
// in a row), stable-sorted so repeated renders don't reshuffle.
export function fmtAttrs(attrs) {
    if (!attrs || typeof attrs !== 'object') return '';
    return Object.keys(attrs).sort().map((k) => k + '=' + fmtVal(attrs[k])).join(' ');
}

function fmtVal(v) {
    if (v === null || v === undefined) return '';
    if (typeof v === 'object') { try { return JSON.stringify(v); } catch (e) { return String(v); } }
    return String(v);
}

// toRow turns a record into the row view-model logs-render.js paints.
export function toRow(rec) {
    const raw = isRaw(rec);
    return {
        ts: rec.ts,
        time: fmtTime(rec.ts),
        service: rec.service || '',
        level: raw ? 'RAW' : (rec.level || 'info').toUpperCase(),
        levelClass: levelClass(rec.level),
        isError: (rec.level || '').toLowerCase() === 'error',
        msg: raw ? rec.raw : (rec.msg || ''),
        attrs: raw ? '' : fmtAttrs(rec.attrs),
    };
}

// toChip turns a ServiceStatus into a chip view-model. up shows time since the
// last log line was seen ("2s ago"); down shows how long it's been down
// ("down 6s"), both relative to nowMs.
export function toChip(status, nowMs) {
    const up = status.state === 'up';
    const now = nowMs || Date.now();
    let meta = '';
    if (up) {
        const seen = status.lastLogSeen ? new Date(status.lastLogSeen).getTime() : NaN;
        meta = isNaN(seen) ? 'no logs yet' : ago(now - seen) + ' ago';
    } else {
        const since = status.since ? new Date(status.since).getTime() : NaN;
        meta = isNaN(since) ? 'down' : 'down ' + ago(now - since);
    }
    return { service: status.service, up, down: !up, meta };
}

// orderChips filters out logd and orders by STATUS_ORDER, appending any unknown
// probed service after the known set (forward-compatible).
export function orderChips(statuses) {
    const list = (statuses || []).filter((s) => s && s.service && s.service !== 'logd');
    return list.slice().sort((a, b) => rank(a.service) - rank(b.service));
}

function rank(service) {
    const i = STATUS_ORDER.indexOf(service);
    return i === -1 ? STATUS_ORDER.length : i;
}

// ago renders a millisecond span as a compact "Ns"/"Nm"/"Nh" string.
function ago(ms) {
    const s = Math.max(0, Math.round(ms / 1000));
    if (s < 60) return s + 's';
    const m = Math.floor(s / 60);
    if (m < 60) return m + 'm';
    return Math.floor(m / 60) + 'h';
}
