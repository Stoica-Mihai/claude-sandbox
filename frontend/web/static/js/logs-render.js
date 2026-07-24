// Logs DOM rendering: builds log rows and status chips from the view-models
// produced by logs-events.js. No fetch/state here — logs.js owns those and
// calls these to paint.

import { toRow, toChip, orderChips } from './logs-events.js';

// makeRow builds one log-row element from a record.
export function makeRow(rec) {
    const vm = toRow(rec);
    const row = document.createElement('div');
    row.className = 'lz-row ' + vm.levelClass + (vm.isError ? ' err' : '');
    row.dataset.ts = vm.ts || '';

    const time = document.createElement('span');
    time.className = 'lz-time';
    time.textContent = vm.time;

    const svc = document.createElement('span');
    svc.className = 'lz-svc';
    svc.textContent = vm.service;

    const lvl = document.createElement('span');
    lvl.className = 'lz-lvl';
    lvl.textContent = vm.level;

    const msg = document.createElement('span');
    msg.className = 'lz-msg';
    if (vm.attrs) {
        // Two child spans (message + muted attrs) instead of a text node so
        // the structure survives the test DOM stub, which drops literal text
        // when children are also present.
        const text = document.createElement('span');
        text.textContent = vm.msg;
        const attr = document.createElement('span');
        attr.className = 'lz-attr';
        attr.textContent = ' ' + vm.attrs;
        msg.appendChild(text);
        msg.appendChild(attr);
    } else {
        msg.textContent = vm.msg;
    }

    row.appendChild(time);
    row.appendChild(svc);
    row.appendChild(lvl);
    row.appendChild(msg);
    return row;
}

// appendRow adds a row at the bottom (newest); prependRow adds at the top
// (older, loaded on scroll-up). Returns the created element.
export function appendRow(listEl, rec) {
    const row = makeRow(rec);
    listEl.appendChild(row);
    return row;
}

export function prependRow(listEl, rec) {
    const row = makeRow(rec);
    listEl.insertBefore(row, listEl.children[0] || null);
    return row;
}

// clearRows removes every rendered row.
export function clearRows(listEl) {
    listEl.textContent = '';
}

// showNote replaces the list content with a centered note (empty / unavailable
// states). isError paints it accent.
export function showNote(listEl, text, isError) {
    listEl.textContent = '';
    const note = document.createElement('div');
    note.className = 'lz-note' + (isError ? ' err' : '');
    note.textContent = text;
    listEl.appendChild(note);
}

// renderStatus rebuilds the status strip from a ServiceStatus list.
export function renderStatus(stripEl, statuses, nowMs) {
    stripEl.textContent = '';
    for (const status of orderChips(statuses)) {
        const chip = toChip(status, nowMs);
        const el = document.createElement('div');
        el.className = 'svc ' + (chip.up ? 'up' : 'down');
        el.dataset.service = chip.service;

        const dot = document.createElement('span');
        dot.className = 'svc-dot';

        const label = document.createElement('span');
        const name = document.createElement('span');
        name.className = 'svc-name';
        name.textContent = chip.service;
        const meta = document.createElement('span');
        meta.className = 'svc-meta';
        meta.textContent = ' · ' + chip.meta;
        label.appendChild(name);
        label.appendChild(meta);

        el.appendChild(dot);
        el.appendChild(label);
        stripEl.appendChild(el);
    }
}
