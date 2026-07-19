// Previous-session delete: two-step confirm state machine inside a history row.
//
// onDeleted(path) is invoked after a successful 204 to refresh the list. The
// caller (picker) passes it in rather than this module importing the renderer —
// that inversion breaks the render↔delete import cycle.

import { stopAnd } from './ui-utils.js';

// Idle state: a trash button inside the .row-act container; click arms the confirm.
export function dpDelToIdle(act, path, uuid, onDeleted) {
    act.classList.remove('confirming', 'failed', 'centered');
    act.style.removeProperty('--row-act-h');
    act.textContent = '';
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'row-act-btn';
    btn.title = 'Delete this conversation permanently';
    btn.innerHTML = '<svg fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="square" stroke-linejoin="miter" d="M6 7h12M9 7V5h6v2m-8 0 1 12h8l1-12"/></svg>';
    btn.onclick = stopAnd(() => dpDelToConfirm(act, path, uuid, onDeleted));
    act.appendChild(btn);
}

// Armed state: accent confirm + ghost cancel. Cancel reverts to idle; confirm deletes.
export function dpDelToConfirm(act, path, uuid, onDeleted) {
    // .centered + --row-act-h: the kit's fixed-height-centered-strip modifier —
    // the confirm/cancel pair doesn't need the row's full (two-line) height.
    act.classList.add('confirming', 'centered');
    act.style.setProperty('--row-act-h', '28px');
    act.textContent = '';

    const yes = document.createElement('button');
    yes.type = 'button';
    yes.className = 'confirm-yes';
    yes.textContent = 'Delete';
    yes.onclick = stopAnd(() => dpDelConfirmed(act, path, uuid, onDeleted));

    const no = document.createElement('button');
    no.type = 'button';
    no.className = 'confirm-no';
    no.textContent = 'Cancel';
    no.onclick = stopAnd(() => dpDelToIdle(act, path, uuid, onDeleted));

    act.appendChild(yes);
    act.appendChild(no);
}

// Confirmed delete: DELETE the conversation; on 204 onDeleted re-renders the list
// (the SSE/broker only refreshes the sidebar, not this modal list).
export async function dpDelConfirmed(act, path, uuid, onDeleted) {
    let res;
    try {
        res = await fetch('/api/sessions/history/' + encodeURIComponent(uuid), { method: 'DELETE' });
    } catch (e) {
        dpDelFail(act, path, uuid, onDeleted);
        return;
    }
    if (res.status === 204) {
        await onDeleted?.(path);
        return;
    }
    dpDelFail(act, path, uuid, onDeleted);
}

// Transient on-brand failure flash, then revert to idle.
export function dpDelFail(act, path, uuid, onDeleted) {
    act.classList.remove('confirming', 'centered');
    act.classList.add('failed');
    act.textContent = '';
    const flash = document.createElement('span');
    flash.className = 'row-act-fail';
    flash.textContent = 'Failed';
    act.appendChild(flash);
    setTimeout(() => dpDelToIdle(act, path, uuid, onDeleted), 1800);
}
