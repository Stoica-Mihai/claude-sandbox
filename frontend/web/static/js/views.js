// Mobile state is owned by CSS via the --is-mobile flag (which flips at the
// breakpoint); reading it here keeps the breakpoint defined in one place: app.css.


// View mode management
// Single terminal view with tabs
let singleTerminalId = null;    // currently active tab
let singleTabs = [];            // array of open tab terminal IDs
// Last-known display name per tab. The tab bar reads names off the sidebar
// cards; when a session dies its card disappears, and without this cache the
// tab label silently degraded to a truncated dtach id on the next render.
const tabNames = {};

// ===== Mobile sidebar drawer =====
function isMobile() {
    return getComputedStyle(document.documentElement).getPropertyValue('--is-mobile').trim() === '1';
}

// ===== Sidebar: collapsible rail (rail ⇄ overlay panel) =====
function applySidebar(expanded) {
    const side = document.getElementById('sidebar');
    const backdrop = document.getElementById('sidebarBackdrop');
    const toggle = document.getElementById('sidebarToggle');
    if (!side) return;
    side.classList.toggle('expanded', expanded);
    if (backdrop) backdrop.classList.toggle('hidden', !expanded);
    if (toggle) toggle.setAttribute('aria-expanded', String(expanded));
    localStorage.setItem('sidebar', expanded ? 'expanded' : 'collapsed');
    requestAnimationFrame(() => TerminalManager.resizeAll());
}

function toggleSidebar() {
    const side = document.getElementById('sidebar');
    applySidebar(!(side && side.classList.contains('expanded')));
}

function collapseSidebar() { applySidebar(false); }

// Open a session terminal
function openSession(terminalId) {
    if (!terminalId) return;
    if (isMobile()) collapseSidebar();
    openSessionSingle(terminalId);
    updateSessionCardStates();
}

function openSessionSingle(terminalId) {
    const wrapper = document.getElementById('singleTerminal');
    if (!wrapper) return;

    // If already open as a tab, just switch to it
    if (singleTabs.includes(terminalId)) {
        switchSingleTab(terminalId);
        return;
    }

    // Create a container div for this tab's terminal
    const tabContainer = document.createElement('div');
    tabContainer.id = 'singleTab-' + terminalId;
    tabContainer.className = 'term-tab hidden';
    tabContainer.classList.add('terminal-bg');
    tabContainer.setAttribute('role', 'tabpanel');
    tabContainer.setAttribute('aria-labelledby', 'ttab-' + terminalId);
    wrapper.appendChild(tabContainer);

    // Add to tabs array
    singleTabs.push(terminalId);

    // Create the terminal in this tab's container
    TerminalManager.create(terminalId, tabContainer);

    // Switch to the new tab
    switchSingleTab(terminalId);
}

function switchSingleTab(terminalId) {
    const wrapper = document.getElementById('singleTerminal');
    if (!wrapper) return;

    // Hide all tab containers, show the selected one
    singleTabs.forEach(id => {
        const el = document.getElementById('singleTab-' + id);
        if (el) el.classList.toggle('hidden', id !== terminalId);
    });

    singleTerminalId = terminalId;

    // Update tab bar, status bar, and sidebar card highlights
    updateSingleTabBar(terminalId);

    updateSessionCardStates();

    // Focus and resize
    requestAnimationFrame(() => {
        const instance = TerminalManager.get(terminalId);
        if (instance) {
            instance.term.focus();
            TerminalManager.resize(terminalId);
        }
    });
}

function closeSingleTab(terminalId) {
    // Destroy the terminal
    TerminalManager.destroy(terminalId);

    // Remove the tab container
    const tabContainer = document.getElementById('singleTab-' + terminalId);
    if (tabContainer) tabContainer.remove();

    // Remove from tabs array
    singleTabs = singleTabs.filter(id => id !== terminalId);
    delete tabNames[terminalId];

    // If this was the active tab, switch to the last remaining tab or show welcome
    if (singleTerminalId === terminalId) {
        if (singleTabs.length > 0) {
            switchSingleTab(singleTabs[singleTabs.length - 1]);
        } else {
            singleTerminalId = null;
            updateSingleTabBar(null);

        }
    } else {
        // Just update the tab bar to remove the closed tab
        updateSingleTabBar(singleTerminalId);
    }
    updateSessionCardStates();
}

function updateSingleWelcome(hasTerminal) {
    const welcome = document.getElementById('singleWelcome');
    const terminal = document.getElementById('singleTerminal');
    const controls = document.getElementById('singleControls');
    const mobileInput = document.getElementById('mobileInputBar');
    if (welcome) welcome.classList.toggle('hidden', hasTerminal);
    if (terminal) terminal.classList.toggle('hidden', !hasTerminal);
    if (controls) {
        // Only show controls bar on desktop (mobile has the input bar)
        if (isMobile()) {
            controls.classList.add('hidden');
        } else {
            controls.classList.toggle('hidden', !hasTerminal);
        }
    }
    if (mobileInput) mobileInput.classList.toggle('hidden', !hasTerminal);
}

// ===== Mobile control bar =====
// Send a single control byte to the active terminal
function sendKeyToTerminal(charCode) {
    if (!singleTerminalId) return;
    const inst = TerminalManager.get(singleTerminalId);
    if (inst?.ws?.readyState === WebSocket.OPEN) {
        inst.ws.send(new Uint8Array([charCode]));
        inst.term?.scrollToBottom();
        inst.term?.focus();
    }
}

// Send an arrow key escape sequence (\x1b[A, \x1b[B, etc.)
function mobileInputSendArrow(code) {
    if (!singleTerminalId) return;
    const inst = TerminalManager.get(singleTerminalId);
    if (inst?.ws?.readyState === WebSocket.OPEN) {
        const encoder = new TextEncoder();
        inst.ws.send(encoder.encode('\x1b[' + code));
        inst.term?.scrollToBottom();
    }
}

// Toggle a selectable text overlay over the terminal (mobile).
function mobileToggleSelect(btn) {
    const terminal = document.getElementById('singleTerminal');
    if (!terminal) return;
    const existing = document.getElementById('selectOverlay');
    if (existing) {
        existing.remove();
        if (btn) btn.classList.remove('sel-active');
        return;
    }

    if (!singleTerminalId) return;
    const inst = TerminalManager.get(singleTerminalId);
    if (!inst) return;

    // Extract visible lines.
    const buf = inst.term.buffer.active;
    const totalLines = buf.baseY + inst.term.rows;
    const lines = [];
    for (let i = 0; i <= totalLines; i++) {
        const line = buf.getLine(i);
        lines.push(line ? line.translateToString(true) : '');
    }
    while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop();

    const overlay = document.createElement('pre');
    overlay.id = 'selectOverlay';
    overlay.textContent = lines.join('\n');
    terminal.appendChild(overlay);
    // Scroll to bottom to match terminal position.
    overlay.scrollTop = overlay.scrollHeight;

    if (btn) btn.classList.add('sel-active');
}

function updateSingleTabBar(activeTerminalId) {
    const tabBar = document.getElementById('singleTabBar');
    if (!tabBar) return;

    if (singleTabs.length === 0) {
        tabBar.innerHTML = '';
        updateSingleWelcome(false);
        return;
    }

    updateSingleWelcome(true);

    tabBar.innerHTML = singleTabs.map(id => {
        const card = document.querySelector(`[data-terminal-id="${id}"]`);
        if (card) tabNames[id] = card.dataset.session;
        const sessionName = card ? card.dataset.session : (tabNames[id] || id.substring(0, 8));
        const dead = !card; // session gone from the sidebar = ended
        const isActive = id === activeTerminalId;
        const safeId = escapeHtml(id);
        return `
            <div class="ttab${isActive ? ' on' : ''}" id="ttab-${safeId}" data-terminal-id="${safeId}"
                 ${dead ? 'title="Session ended"' : ''}
                 role="tab" aria-selected="${isActive}" aria-controls="singleTab-${safeId}" tabindex="${isActive ? '0' : '-1'}"
                 onclick="switchSingleTab('${safeId}')"
                 onauxclick="if(event.button===1){event.preventDefault();closeSingleTab('${safeId}');}">
                <span class="tdot${dead ? ' dead' : isActive ? ' on' : ''}"></span>
                <span style="font-family:var(--mono);font-style:normal">${escapeHtml(sessionName)}</span>
                <button type="button" class="x" tabindex="-1" aria-label="Close tab" onclick="closeSingleTab('${safeId}'); event.stopPropagation();">&times;</button>
            </div>`;
    }).join('');
}

// Keyboard contract for the tab strip (role=tablist/tab, roving tabindex) —
// mirrors the kit's .tabs/fdTab pattern (ArrowLeft/Right move+activate,
// Enter/Space activate) by hand rather than adopting .tabs/fdInit, since this
// app hand-wires its own interaction logic throughout rather than depending
// on futurism.js. Delete/Backspace-to-close has no kit precedent (the kit's
// plain tab has no close affordance) — an app-original extension.
document.addEventListener('keydown', (e) => {
    const tab = e.target.closest && e.target.closest('.ttab[role="tab"]');
    if (!tab) return;
    const bar = document.getElementById('singleTabBar');
    if (!bar) return;
    const tabs = Array.from(bar.querySelectorAll('.ttab[role="tab"]'));
    const i = tabs.indexOf(tab);
    const id = tab.dataset.terminalId;

    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
        e.preventDefault();
        const n = e.key === 'ArrowRight' ? (i + 1) % tabs.length : (i - 1 + tabs.length) % tabs.length;
        const nextId = tabs[n].dataset.terminalId;
        switchSingleTab(nextId);
        document.getElementById('ttab-' + nextId)?.focus();
    } else if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        switchSingleTab(id);
        document.getElementById('ttab-' + id)?.focus();
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
        e.preventDefault();
        closeSingleTab(id);
        if (singleTerminalId) {
            document.getElementById('ttab-' + singleTerminalId)?.focus();
        } else {
            document.querySelector('.btn-new')?.focus();
        }
    }
});

function updateSessionCardStates() {
    document.querySelectorAll('.session-card').forEach(card => {
        const tid = card.dataset.terminalId;
        const active = singleTabs.includes(tid) && tid === singleTerminalId;
        card.classList.toggle('active', active);
        // aria-current mirrors the kit's .nav-v active semantics for screen readers.
        if (active) card.setAttribute('aria-current', 'page');
        else card.removeAttribute('aria-current');
    });
}

// Session card click delegation — handles all clicks on session cards
document.addEventListener('click', (e) => {
    const card = e.target.closest('.session-card');
    if (!card) return;

    // Do not intercept button clicks inside card
    if (e.target.closest('button')) return;

    const terminalId = card.dataset.terminalId;
    if (!terminalId) return;

    // Regular click: open in single view
    openSession(terminalId);
});

// Keyboard activation: the card is role="button" tabindex="0", so Enter/Space
// open it — but only when the card itself is focused, not a nested action button.
document.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const card = e.target.closest && e.target.closest('.session-card');
    if (!card || card !== e.target) return;
    e.preventDefault();
    const terminalId = card.dataset.terminalId;
    if (terminalId) openSession(terminalId);
});

// Utility: escape HTML for safe insertion
function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

// Clean up terminal tab/view when a session is killed from the sidebar
function cleanupKilledSession(terminalId) {
    // Close from single view tabs
    if (singleTabs.includes(terminalId)) {
        closeSingleTab(terminalId);
    }
}

// Skeleton placeholder rows (kit .skel) shown while a fetch is in flight.
function dpSkelRows(count, height) {
    let html = '';
    for (let i = 0; i < count; i++) {
        html += `<div class="skel" style="height:${height}px;margin:8px 12px"></div>`;
    }
    return html;
}

// Open the New Session modal, resetting the picker to a fresh browse state
// (re-fetch the root folder list) so it never reopens in a stale/selected state.
function openNewSessionModal(event) {
    document.getElementById('newSessionModal').showModal();
    if (window.htmx) {
        const picker = document.getElementById('dir-picker');
        if (picker) picker.innerHTML = ''; // clear stale content from the last open
        htmx.ajax('GET', '/api/directories', { target: '#dir-picker', swap: 'innerHTML' });
    }
}

// Show folder skeletons while htmx swaps the directory picker (open, breadcrumb,
// drill — all target #dir-picker; the real list replaces them). Delay the paint
// so fast/cached responses never flash a skeleton; afterRequest cancels it.
let dpDirSkelTimer = null;
document.addEventListener('htmx:beforeRequest', (e) => {
    if (e.detail?.target?.id !== 'dir-picker') return;
    const target = e.detail.target;
    clearTimeout(dpDirSkelTimer);
    dpDirSkelTimer = setTimeout(() => { target.innerHTML = dpSkelRows(5, 36); }, 150);
});
document.addEventListener('htmx:afterRequest', (e) => {
    if (e.detail?.target?.id === 'dir-picker') clearTimeout(dpDirSkelTimer);
});

// Open the Rename Session modal
let renameTargetId = null;
function openRenameModal(terminalId, currentName) {
    renameTargetId = terminalId;
    const input = document.getElementById('renameInput');
    input.value = currentName || '';
    document.getElementById('renameModal').showModal();
    setTimeout(() => { input.focus(); input.select(); }, 50);
}
document.getElementById('renameSubmit')?.addEventListener('click', () => {
    if (!renameTargetId) return;
    const name = document.getElementById('renameInput').value.trim();
    const targetId = renameTargetId;
    fetch(`/api/sessions/${targetId}/name`, {
        method: 'PUT',
        body: JSON.stringify({ name })
    }).then(res => {
        if (!res.ok) throw new Error(`Rename failed (${res.status})`);
        document.getElementById('renameModal').close();
        renameTargetId = null;
    }).catch(err => {
        console.error('Rename failed:', err);
        document.getElementById('renameInput').classList.add('err-flash');
        setTimeout(() => {
            const el = document.getElementById('renameInput');
            if (el) el.classList.remove('err-flash');
        }, 2000);
    });
});
// Submit on Enter key
document.getElementById('renameInput')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        e.preventDefault();
        document.getElementById('renameSubmit').click();
    }
});

// Re-apply session card active states after HTMX swaps in fresh session list HTML
document.addEventListener('htmx:afterSwap', (event) => {
    if (event.target?.id === 'session-list') {
        updateSessionCardStates();
        tickDurations(); // fill the freshly-swapped cards' duration immediately
        // Re-render the tab bar from the fresh cards: renames propagate to tab
        // labels, and tabs whose session ended get their dead marker. Skip when
        // focus is inside the strip so an SSE update can't yank keyboard focus.
        const bar = document.getElementById('singleTabBar');
        if (singleTabs.length && bar && !bar.contains(document.activeElement)) {
            updateSingleTabBar(singleTerminalId);
        }
    }
    if (event.target?.id === 'dir-picker') {
        dpResetBrowse(); // a fresh folder-list level → browse state, nothing selected
    }
});

// --- New Session modal: browse folders → select one → start new / resume ---

let dirPickerSel = { kind: null, uuid: null };

function relTime(unix) {
    const s = Math.floor(Date.now() / 1000) - unix;
    if (s < 60) return s + 's ago';
    if (s < 3600) return Math.floor(s / 60) + 'm ago';
    if (s < 86400) return Math.floor(s / 3600) + 'h ago';
    return Math.floor(s / 86400) + 'd ago';
}

// Format an elapsed-seconds count for a session card's duration: "2h 15m" / "45s".
// The client owns this format — the server no longer renders it.
function fmtDuration(sec) {
    let s = sec;
    const h = Math.floor(s / 3600); s %= 3600;
    const m = Math.floor(s / 60); s %= 60;
    if (h > 0) return m > 0 ? h + 'h ' + m + 'm' : h + 'h';
    if (m > 0) return s > 0 ? m + 'm ' + s + 's' : m + 'm';
    return s + 's';
}

// Refresh every session card's live duration from its data-created stamp.
function tickDurations() {
    const now = Math.floor(Date.now() / 1000);
    document.querySelectorAll('.session-duration[data-created]').forEach(el => {
        const ts = parseInt(el.dataset.created, 10);
        if (ts) el.textContent = fmtDuration(now - ts);
    });
}

function dpFooter(label, enabled) {
    const b = document.getElementById('dir-picker-submit');
    if (!b) return;
    b.textContent = label;
    b.disabled = !enabled;
}

// Browse state: folder list + new-project row visible, no folder selected, action disabled.
function dpResetBrowse() {
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
// openEditor/closeEditor/createProject are invoked from the fragment's onclick
// attrs; each may be handed the .newrow or a button inside .newedit, so resolve
// both siblings from the picker rather than trusting the passed element.

function dpEditorEls() {
    const picker = document.getElementById('dir-picker');
    if (!picker) return null;
    const newrow = picker.querySelector('.newrow');
    const editor = picker.querySelector('.newedit');
    if (!newrow || !editor) return null;
    return { newrow, editor, input: editor.querySelector('.dp-newname'), errline: editor.querySelector('.errline') };
}

function dpEditorClearError(els) {
    if (els.errline) { els.errline.textContent = ''; els.errline.classList.add('hidden'); }
    if (els.input) els.input.classList.remove('err-flash');
}

// Inline error affordance shared by the client pre-check and the server 400/409
// path: fill + reveal .errline, flash the input outline. Cleared on next
// keystroke or on close (no auto-timeout, unlike the fire-and-forget rename flash).
function dpEditorShowError(els, msg) {
    if (els.errline) { els.errline.textContent = msg; els.errline.classList.remove('hidden'); }
    if (els.input) els.input.classList.add('err-flash');
}

function openEditor() {
    const els = dpEditorEls();
    if (!els) return;
    els.newrow.classList.add('hidden');
    els.editor.classList.remove('hidden');
    dpEditorClearError(els);
    if (els.input) { els.input.value = ''; setTimeout(() => els.input.focus(), 0); }
}

function closeEditor() {
    const els = dpEditorEls();
    if (!els) return;
    els.editor.classList.add('hidden');
    els.newrow.classList.remove('hidden');
    if (els.input) els.input.value = '';
    dpEditorClearError(els);
}

const dpNameRe = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

// Read + validate the name (UX only; server is authoritative), then POST. The
// browse path comes off the newrow's data-dp-* (Decision 10), not breadcrumbs.
async function createProject() {
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

// Kit toast (CSS lives in futurism.css; the .toaster host is built on demand).
// For notices that must outlive the modal, e.g. "created, git init failed".
function dpToast(msg) {
    let host = document.querySelector('.toaster');
    if (!host) {
        host = document.createElement('div');
        host.className = 'toaster';
        document.body.appendChild(host);
    }
    const el = document.createElement('div');
    el.className = 'toast err';
    el.setAttribute('role', 'status');
    el.textContent = msg;
    host.appendChild(el);
    setTimeout(() => el.classList.add('out'), 3600);
    setTimeout(() => el.remove(), 4000);
}

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

// Choose start-new vs a previous session; relabel the footer in place.
function dirPickerSetSel(kind, uuid, rowEl) {
    dirPickerSel = { kind, uuid: uuid || null };
    document.querySelectorAll('#session-actions .sa-row').forEach(r => r.classList.remove('sa-sel'));
    if (rowEl) rowEl.classList.add('sa-sel');
    const resume = document.getElementById('dir-picker-resume');
    if (resume) resume.value = kind === 'resume' ? uuid : '';
    dpFooter(kind === 'resume' ? 'Resume' : 'Launch', true);
}

// Selecting a folder hides the folder list and shows that folder's start-new +
// previous sessions. (The › arrow on a row drills into subfolders instead.)
async function dpSelectFolder(path, name) {
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
    newRow.style.cssText = 'width:100%;background:var(--row-bg,transparent);border:none;text-align:left;font-family:inherit;color:inherit';
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
async function dpRenderHistory(path) {
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
            row.style.cssText = 'width:100%;background:var(--row-bg,transparent);border:none;border-bottom:2px solid var(--line);text-align:left;font-family:inherit;color:inherit';
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
    btn.onclick = (e) => {
        e.stopPropagation();
        e.preventDefault();
        dpDelToConfirm(act, path, uuid);
    };
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
    yes.onclick = (e) => {
        e.stopPropagation();
        e.preventDefault();
        dpDelConfirmed(act, path, uuid);
    };

    const no = document.createElement('button');
    no.type = 'button';
    no.className = 'confirm-no';
    no.textContent = 'Cancel';
    no.onclick = (e) => {
        e.stopPropagation();
        e.preventDefault();
        dpDelToIdle(act, path, uuid);
    };

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
            submitBtn.textContent = 'Failed — try again';
            setTimeout(() => {
                submitBtn.classList.remove('btn-spawn-fail');
                submitBtn.textContent = dirPickerSel.kind === 'resume' ? 'Resume' : 'Launch';
            }, 2000);
        }
        return;
    }

    document.getElementById('newSessionModal')?.close();
    openSession(terminalId);
});

// ===== Pull to refresh =====
function initPullToRefresh() {
    const body = document.body;
    const indicator = document.getElementById('pullIndicator');
    if (!indicator) return;
    const label = indicator.querySelector('.pull-label');

    const threshold = 80;   // px pull to arm a refresh
    const maxPull = 100;    // px where the bar reaches full height
    const barHeight = 40;   // px bar height when fully revealed
    let startY = 0;
    let pulling = false;

    const reset = () => {
        indicator.style.height = '0';
        indicator.style.setProperty('--pull', '0');
        indicator.classList.remove('armed', 'refreshing');
    };

    body.addEventListener('touchstart', (e) => {
        // Don't activate inside terminals, open dialogs, or the sidebar drawer
        if (e.target.closest('.xterm') || e.target.closest('#singleTerminal')) return;
        if (document.querySelector('dialog[open]')) return;
        if (e.target.closest('#sidebar')) return;
        startY = e.touches[0].clientY;
        pulling = true;
    }, { passive: true });

    body.addEventListener('touchmove', (e) => {
        if (!pulling) return;
        const dy = e.touches[0].clientY - startY;
        if (dy < 0) { pulling = false; reset(); return; }
        const clamped = Math.min(dy, maxPull);
        indicator.style.height = (clamped / maxPull * barHeight) + 'px';
        indicator.style.setProperty('--pull', String(Math.min(dy / threshold, 1)));
        const armed = dy >= threshold;
        indicator.classList.toggle('armed', armed);
        if (label) label.textContent = armed ? 'Release to refresh' : 'Pull to refresh';
    }, { passive: true });

    body.addEventListener('touchend', () => {
        if (!pulling) return;
        pulling = false;
        if (indicator.classList.contains('armed')) {
            indicator.classList.add('refreshing');
            indicator.style.height = barHeight + 'px';
            if (label) label.textContent = 'Refreshing';
            setTimeout(() => location.reload(), 400);
        } else {
            reset();
        }
    });
}

// ===== Keyboard shortcuts (desktop only) =====
function handleShortcuts(e) {
    // Guard: do not fire when a modal dialog is open
    if (document.querySelector('dialog[open]')) return;

    // Guard: do not fire when focus is in an input/textarea (non-terminal)
    const tag = document.activeElement?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') {
        if (!document.activeElement.classList.contains('xterm-helper-textarea')) {
            return;
        }
    }

    const key = e.key;

    // Alt+N — Open new session modal
    if (e.altKey && key === 'n') {
        e.preventDefault();
        openNewSessionModal({});
        return;
    }

    // Alt+W — Close current tab
    if (e.altKey && key === 'w') {
        e.preventDefault();
        if (singleTerminalId) {
            closeSingleTab(singleTerminalId);
        }
        return;
    }

    // Alt+1 through Alt+9 — Switch to tab N
    if (e.altKey) {
        const digit = parseInt(key, 10);
        if (digit >= 1 && digit <= 9) {
            e.preventDefault();
            const index = digit - 1;
            if (index < singleTabs.length) {
                switchSingleTab(singleTabs[index]);
            }
            return;
        }
    }
}

document.getElementById('sidebarBackdrop')?.addEventListener('click', collapseSidebar);
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !document.querySelector('dialog[open]')) collapseSidebar();
});

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    applySidebar(localStorage.getItem('sidebar') === 'expanded');
    initPullToRefresh();

    // Register keyboard shortcuts on desktop only
    if (!isMobile()) {
        document.addEventListener('keydown', handleShortcuts);
    }

    // Fill durations now, then tick every second (client owns the format).
    tickDurations();
    setInterval(tickDurations, 1000);
});
