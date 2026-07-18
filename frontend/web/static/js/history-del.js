// Previous-session delete: two-step confirm state machine inside a history row.

// Idle state: a trash button inside the .row-act container; click arms the confirm.
function dpDelToIdle(act, path, uuid) {
    act.classList.remove('confirming', 'failed', 'centered');
    act.style.removeProperty('--row-act-h');
    act.textContent = '';
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'row-act-btn';
    btn.title = 'Delete this conversation permanently';
    btn.innerHTML = '<svg fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="square" stroke-linejoin="miter" d="M6 7h12M9 7V5h6v2m-8 0 1 12h8l1-12"/></svg>';
    btn.onclick = stopAnd(() => dpDelToConfirm(act, path, uuid));
    act.appendChild(btn);
}

// Armed state: accent confirm + ghost cancel. Cancel reverts to idle; confirm deletes.
function dpDelToConfirm(act, path, uuid) {
    // .centered + --row-act-h: the kit's fixed-height-centered-strip modifier —
    // the confirm/cancel pair doesn't need the row's full (two-line) height.
    act.classList.add('confirming', 'centered');
    act.style.setProperty('--row-act-h', '28px');
    act.textContent = '';

    const yes = document.createElement('button');
    yes.type = 'button';
    yes.className = 'confirm-yes';
    yes.textContent = 'Delete';
    yes.onclick = stopAnd(() => dpDelConfirmed(act, path, uuid));

    const no = document.createElement('button');
    no.type = 'button';
    no.className = 'confirm-no';
    no.textContent = 'Cancel';
    no.onclick = stopAnd(() => dpDelToIdle(act, path, uuid));

    act.appendChild(yes);
    act.appendChild(no);
}

// Confirmed delete: DELETE the conversation; on 204 the history re-render is the
// source of truth (the SSE/broker only refreshes the sidebar, not this modal list).
async function dpDelConfirmed(act, path, uuid) {
    let res;
    try {
        res = await fetch('/api/sessions/history/' + encodeURIComponent(uuid), { method: 'DELETE' });
    } catch (e) {
        dpDelFail(act, path, uuid);
        return;
    }
    if (res.status === 204) {
        await dpRenderHistory(path);
        return;
    }
    dpDelFail(act, path, uuid);
}

// Transient on-brand failure flash, then revert to idle.
function dpDelFail(act, path, uuid) {
    act.classList.remove('confirming', 'centered');
    act.classList.add('failed');
    act.textContent = '';
    const flash = document.createElement('span');
    flash.className = 'row-act-fail';
    flash.textContent = 'Failed';
    act.appendChild(flash);
    setTimeout(() => dpDelToIdle(act, path, uuid), 1800);
}
