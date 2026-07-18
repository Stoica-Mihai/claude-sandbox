// New Session modal: directory picker, inline new-project editor, spawn wiring.

import { escapeHtml, setBtnLabel, dpSkelRows, AROW_CSS, relTime, dpToast } from './ui-utils.js';
import { dpDelToIdle } from './history-del.js';
import { register } from './actions.js';

// openSession is wired onto globalThis by main.js (tabs.openSession); read it
// late so a spawn response opens the terminal, and so tests can stub it.

// --- New Session modal: browse folders → select one → start new / resume ---

let dirPickerSel = { kind: null, uuid: null };
let dpDirSkelTimer = null;

// Open the New Session modal, resetting the picker to a fresh browse state
// (re-fetch the root folder list) so it never reopens in a stale/selected state.
export function openNewSessionModal(event) {
    document.getElementById('newSessionModal').showModal();
    if (window.htmx) {
        const picker = document.getElementById('dir-picker');
        if (picker) picker.innerHTML = ''; // clear stale content from the last open
        htmx.ajax('GET', '/api/directories', { target: '#dir-picker', swap: 'innerHTML' });
    }
}

export function dpFooter(label, enabled) {
    const b = document.getElementById('dir-picker-submit');
    if (!b) return;
    setBtnLabel(b, label);
    b.disabled = !enabled;
}

// Browse state: folder list + new-project row visible, no folder selected, action disabled.
export function dpResetBrowse() {
    dirPickerSel = { kind: null, uuid: null };
    const actions = document.getElementById('session-actions');
    if (actions) actions.innerHTML = '';
    const folders = document.getElementById('dp-folders');
    if (folders) folders.classList.remove('hidden');
    const els = dpEditorEls();
    if (els) { els.newrow.classList.remove('hidden'); els.editor.classList.add('hidden'); }
    const cwd = document.getElementById('dir-picker-cwd');
    if (cwd) cwd.value = '';
    const resume = document.getElementById('dir-picker-resume');
    if (resume) resume.value = '';
    dpFooter('Launch', false);
}

// --- New Project inline editor (in-fragment state machine) ---
// The fragment holds one .newrow (idle) + one .newedit (form) under #dp-folders.
// openEditor/closeEditor/createProject are invoked from the fragment's data-action
// attrs; each resolves both siblings from the picker rather than trusting any
// passed element.

export function dpEditorEls() {
    const picker = document.getElementById('dir-picker');
    if (!picker) return null;
    const newrow = picker.querySelector('.newrow');
    const editor = picker.querySelector('.newedit');
    if (!newrow || !editor) return null;
    return { newrow, editor, input: editor.querySelector('.dp-newname'), errline: editor.querySelector('.errline') };
}

export function dpEditorClearError(els) {
    if (els.errline) { els.errline.textContent = ''; els.errline.classList.add('hidden'); }
    if (els.input) els.input.classList.remove('err-flash');
}

// Inline error affordance shared by the client pre-check and the server 400/409
// path: fill + reveal .errline, flash the input outline. Cleared on next
// keystroke or on close (no auto-timeout, unlike the fire-and-forget rename flash).
export function dpEditorShowError(els, msg) {
    if (els.errline) { els.errline.textContent = msg; els.errline.classList.remove('hidden'); }
    if (els.input) els.input.classList.add('err-flash');
}

export function openEditor() {
    const els = dpEditorEls();
    if (!els) return;
    els.newrow.classList.add('hidden');
    els.editor.classList.remove('hidden');
    dpEditorClearError(els);
    if (els.input) { els.input.value = ''; setTimeout(() => els.input.focus(), 0); }
}

export function closeEditor() {
    const els = dpEditorEls();
    if (!els) return;
    els.editor.classList.add('hidden');
    els.newrow.classList.remove('hidden');
    if (els.input) els.input.value = '';
    dpEditorClearError(els);
}

// Pattern injected by layout.html from shared/enums.go (single source with the
// backend validator). Without the injection the pre-check accepts anything and
// defers to the server — the pattern is never duplicated here.
const dpNameRe = new RegExp(
    (typeof window !== 'undefined' && window.NEW_PROJECT_NAME_PATTERN) || '.*'
);

// Read + validate the name (UX only; server is authoritative), then POST. The
// browse path comes off the newrow's data-dp-* (Decision 10), not breadcrumbs.
export async function createProject() {
    const els = dpEditorEls();
    if (!els || !els.input) return;

    const name = els.input.value.trim();
    const gitInit = !!els.editor.querySelector('.dp-gitinit')?.checked;
    const path = els.newrow.dataset.dpPath || '';
    const full = els.newrow.dataset.dpFull || '';

    if (!dpNameRe.test(name)) {
        dpEditorShowError(els, 'Invalid name');
        return;
    }
    const dup = Array.from(document.querySelectorAll('#dp-folders .fnm'))
        .some(el => el.textContent.trim() === name);
    if (dup) {
        dpEditorShowError(els, 'Folder already exists');
        return;
    }

    let res;
    try {
        res = await fetch('/api/directories', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path, name, gitInit }),
        });
    } catch (e) {
        dpEditorShowError(els, 'Could not create — check the connection.');
        return;
    }

    if (res.status === 201) {
        let warning = '';
        try { warning = (await res.json()).warning || ''; } catch (e) { /* body optional */ }
        if (warning) dpToast('created, git init failed');
        // A fresh folder has nothing to resume — skip the select+LAUNCH step
        // and spawn directly; the spawn response's X-Terminal-Id handler
        // closes the modal and opens the terminal tab.
        document.getElementById('dir-picker-cwd').value = full + '/' + name;
        const resume = document.getElementById('dir-picker-resume');
        if (resume) resume.value = '';
        document.getElementById('dir-picker-form')?.requestSubmit();
        return;
    }

    if (res.status === 400 || res.status === 409) {
        let msg = 'Could not create the folder.';
        try { msg = (await res.json()).error || msg; } catch (e) { /* keep generic */ }
        dpEditorShowError(els, msg);
        return;
    }

    dpEditorShowError(els, 'Could not create the folder.');
}

// Choose start-new vs a previous session; relabel the footer in place.
export function dirPickerSetSel(kind, uuid, rowEl) {
    dirPickerSel = { kind, uuid: uuid || null };
    document.querySelectorAll('#session-actions .sa-row').forEach(r => r.classList.remove('sa-sel'));
    if (rowEl) rowEl.classList.add('sa-sel');
    const resume = document.getElementById('dir-picker-resume');
    if (resume) resume.value = kind === 'resume' ? uuid : '';
    dpFooter(kind === 'resume' ? 'Resume' : 'Launch', true);
}

// Selecting a folder hides the folder list and shows that folder's start-new +
// previous sessions. (The › arrow on a row drills into subfolders instead.)
export async function dpSelectFolder(path, name) {
    document.getElementById('dir-picker-cwd').value = path;
    const folders = document.getElementById('dp-folders');
    if (folders) folders.classList.add('hidden');
    // Selected state shows only the crumb + actions: the new-project affordance
    // belongs to browse mode (dpResetBrowse restores it).
    const els = dpEditorEls();
    if (els) { els.newrow.classList.add('hidden'); els.editor.classList.add('hidden'); }

    // Append the selected folder to the breadcrumb as the current (non-link) crumb.
    const bc = document.getElementById('dp-breadcrumb');
    if (bc && !bc.querySelector('.dp-cur')) {
        const sep = document.createElement('span');
        sep.className = 'sep';
        sep.textContent = '/';
        const cur = document.createElement('span');
        cur.className = 'dp-cur seg cur';
        cur.setAttribute('aria-current', 'page');
        cur.textContent = name;
        bc.appendChild(sep);
        bc.appendChild(cur);
    }

    const actions = document.getElementById('session-actions');
    actions.innerHTML = '';
    const newRow = document.createElement('button');
    newRow.type = 'button';
    newRow.className = 'arow sa-row';
    newRow.style.cssText = AROW_CSS;
    newRow.innerHTML = '<div class="atxt"><div class="at1">Start a new session</div>'
        + '<div class="at2">Fresh conversation in ' + escapeHtml(path) + '</div></div>';
    newRow.onclick = () => dirPickerSetSel('new', null, newRow);
    actions.appendChild(newRow);

    await dpRenderHistory(path);

    dirPickerSetSel('new', null, newRow); // default to "start new"
}

// Fetch + render the "Previous sessions" list for a folder. Re-invokable after a
// delete: it strips the existing label + rows and rebuilds them in place, keeping
// the "Start a new session" row above untouched.
export async function dpRenderHistory(path) {
    const actions = document.getElementById('session-actions');
    if (!actions) return;

    actions.querySelectorAll('.actitle, .row-host, .empty-state, .dp-skel').forEach(el => el.remove());

    // Delay the skeleton so fast responses never flash it; a single block
    // (not a counted list) so it doesn't promise a row count it can't know.
    const skelTimer = setTimeout(() => {
        const s = document.createElement('div');
        s.className = 'dp-skel';
        s.innerHTML = dpSkelRows(1, 52);
        actions.appendChild(s);
    }, 150);

    let entries = [];
    let failed = false;
    try {
        const res = await fetch('/api/sessions/history?cwd=' + encodeURIComponent(path));
        if (res.ok) entries = await res.json();
        else failed = true;
    } catch (e) { failed = true; }

    clearTimeout(skelTimer);
    actions.querySelectorAll('.dp-skel').forEach(el => el.remove());

    const label = document.createElement('div');
    label.className = 'actitle';
    label.textContent = 'Previous sessions';
    actions.appendChild(label);

    if (failed) {
        const err = document.createElement('div');
        err.className = 'empty-state';
        err.textContent = 'Could not load previous sessions — check the connection.';
        actions.appendChild(err);
    } else if (entries.length) {
        entries.forEach(s => {
            const short = (s.uuid || '').slice(0, 8);
            const title = s.name ? s.name : relTime(s.created);
            const sub = s.name ? (relTime(s.created) + ' · ' + short) : short;

            const wrap = document.createElement('div');
            wrap.className = 'row-host';

            const row = document.createElement('button');
            row.type = 'button';
            row.className = 'arow sa-row';
            row.style.cssText = AROW_CSS + ';border-bottom:2px solid var(--line)';
            row.innerHTML = '<svg class="aold" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="square" stroke-linejoin="miter" d="M8 10h8M8 14h5M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>'
                + '<div class="atxt"><div class="at1">' + escapeHtml(title) + '</div>'
                + '<div class="at2">' + escapeHtml(sub) + '</div></div>';
            row.onclick = () => dirPickerSetSel('resume', s.uuid, row);

            // Kit .row-act is a CONTAINER (div), not a button — its children are
            // buttons, so nothing nests a button inside a button.
            const act = document.createElement('div');
            act.className = 'row-act';
            dpDelToIdle(act, path, s.uuid);

            wrap.appendChild(row);
            wrap.appendChild(act);
            actions.appendChild(wrap);
        });
    } else {
        const empty = document.createElement('div');
        empty.className = 'empty-state';
        empty.innerHTML = 'No previous sessions in this folder'
            + '<br><span style="opacity:.7">Start a new one above — it\'ll show here next time.</span>';
        actions.appendChild(empty);
    }
}

export function init() {
    dirPickerSel = { kind: null, uuid: null };
    dpDirSkelTimer = null;

    register('new-session', () => openNewSessionModal({}));
    register('dp-select-folder', (el) => dpSelectFolder(el.dataset.path, el.dataset.name));
    register('dp-open-editor', () => openEditor());
    register('dp-close-editor', () => closeEditor());
    register('dp-create-project', () => createProject());

    // Show folder skeletons while htmx swaps the directory picker (open, breadcrumb,
    // drill — all target #dir-picker; the real list replaces them). Delay the paint
    // so fast/cached responses never flash a skeleton; afterRequest cancels it.
    document.addEventListener('htmx:beforeRequest', (e) => {
        if (e.detail?.target?.id !== 'dir-picker') return;
        const target = e.detail.target;
        clearTimeout(dpDirSkelTimer);
        dpDirSkelTimer = setTimeout(() => { target.innerHTML = dpSkelRows(5, 36); }, 150);
    });
    document.addEventListener('htmx:afterRequest', (e) => {
        if (e.detail?.target?.id === 'dir-picker') clearTimeout(dpDirSkelTimer);
    });

    // Delegated on document so the handlers survive every #dir-picker fragment swap
    // (the input is re-rendered each time; per-element binding would be lost).
    document.addEventListener('keydown', (e) => {
        if (!e.target.classList?.contains('dp-newname')) return;
        if (e.key === 'Enter') {
            e.preventDefault();
            createProject();
        } else if (e.key === 'Escape') {
            // preventDefault blocks the native <dialog> Esc-cancel (a keydown default
            // action, which stopPropagation alone cannot stop); close only the editor.
            e.preventDefault();
            e.stopPropagation();
            closeEditor();
        }
    });
    document.addEventListener('input', (e) => {
        if (!e.target.classList?.contains('dp-newname')) return;
        const els = dpEditorEls();
        if (els) dpEditorClearError(els);
    });

    document.addEventListener('htmx:afterSwap', (event) => {
        if (event.target?.id === 'dir-picker') {
            dpResetBrowse(); // a fresh folder-list level → browse state, nothing selected
        }
    });

    // Listen for HTMX responses from spawn to auto-open the new terminal
    document.addEventListener('htmx:afterRequest', (event) => {
        const xhr = event.detail.xhr;
        if (!xhr) return;

        const terminalId = xhr.getResponseHeader('X-Terminal-Id');
        if (!terminalId) return; // not a spawn response

        if (xhr.status >= 400) {
            const submitBtn = document.getElementById('dir-picker-submit');
            if (submitBtn) {
                submitBtn.classList.add('btn-spawn-fail');
                setBtnLabel(submitBtn, 'Failed — try again');
                setTimeout(() => {
                    submitBtn.classList.remove('btn-spawn-fail');
                    setBtnLabel(submitBtn, dirPickerSel.kind === 'resume' ? 'Resume' : 'Launch');
                }, 2000);
            }
            return;
        }

        document.getElementById('newSessionModal')?.close();
        openSession(terminalId);
    });
}
