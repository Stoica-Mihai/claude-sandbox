// Settings editor: GET/PUT the whitelisted preference subset of container-settings.json.
// Applies to NEW sessions (claude reads settings at spawn).

import { isMobile } from './ui-utils.js';
import { refreshShareStatus } from './share.js';
import { fdSyncToggle, toggleThinking } from './theme.js';
import { register } from './actions.js';

// Custom select: open on value click, pick sets value + closes; click-outside closes.
// An option's value is its data-value when present (label != value, e.g. the
// advisor's "Opus 4.8" -> "claude-opus-4-8"), otherwise its label text.
function optValue(o) { return o.dataset.value !== undefined ? o.dataset.value : o.textContent; }

const SETTINGS_HINT = 'Applies to new sessions';
// Clear a previous save error from the footer when the user changes anything.
function clearSettingsError() {
    const h = document.getElementById('settings-hint');
    if (h && h.classList.contains('err')) { h.textContent = SETTINGS_HINT; h.classList.remove('err'); }
}

// Mark opt as the chosen option within sel (clearing siblings), sync
// aria-selected, and show label.
function applyOption(sel, opt, label) {
    sel.querySelectorAll('.sel-opt').forEach(o => {
        const on = o === opt;
        o.classList.toggle('sel-on', on);
        o.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    sel.querySelector('.sel-cur').textContent = label;
}

// Open/close a .sel, keeping aria-expanded in sync — ported from the kit's
// fdSelOpen contract (futurism.js), since futurism.js itself isn't vendored.
// Opening one closes any other open .sel in the modal (one dropdown at a time)
// and focuses the current/first option; closing restores focus to the trigger.
function setSelOpen(sel, open) {
    if (open) {
        document.querySelectorAll('#settingsModal .sel.open').forEach(o => {
            if (o !== sel) setSelOpen(o, false);
        });
    }
    sel.classList.toggle('open', open);
    const val = sel.querySelector('.sel-val');
    if (val) val.setAttribute('aria-expanded', open ? 'true' : 'false');
    if (open) {
        const cur = sel.querySelector('.sel-opt.sel-on') || sel.querySelector('.sel-opt');
        if (cur) cur.focus();
    } else if (val) {
        val.focus();
    }
}

export function settingsPick(opt) {
    const sel = opt.closest('.sel');
    applyOption(sel, opt, opt.textContent);
    sel.dataset.value = optValue(opt);
    setSelOpen(sel, false);
    clearSettingsError();
}

// Set a .sel to a value (matched against each option's data-value/label),
// showing that option's label and storing the value on the element.
function setSel(field, value) {
    const sel = document.querySelector(`#settingsModal .sel[data-field="${field}"]`);
    if (!sel) return;
    value = value || '';
    sel.dataset.value = value;
    let match = null;
    sel.querySelectorAll('.sel-opt').forEach(o => { if (optValue(o) === value) match = o; });
    applyOption(sel, match, match ? match.textContent : (value || '—'));
}

function getSel(field) {
    const sel = document.querySelector(`#settingsModal .sel[data-field="${field}"]`);
    return sel ? (sel.dataset.value || '') : '';
}

// Switch the visible settings category. Only Session persists via SAVE; the
// Appearance (accent) and Sharing (tunnel) panels act instantly, so the footer
// SAVE button and hint are shown for Session only. Sharing is off-limits on
// mobile — a mobile user is usually the tunnel client, and a mis-tap on GO
// PRIVATE / regenerate would disconnect them.
export function settingsSelectCategory(cat) {
    if (cat === 'sharing' && isMobile()) return;
    document.querySelectorAll('#settingsModal .snav').forEach(b => {
        const on = b.dataset.cat === cat;
        b.classList.toggle('active', on);
        b.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    document.querySelectorAll('#settingsModal .settings-panel').forEach(p => {
        p.hidden = p.dataset.cat !== cat;
    });
    const save = document.getElementById('settings-save');
    const hint = document.getElementById('settings-hint');
    if (save) save.classList.toggle('hidden', cat !== 'session');
    if (hint) hint.classList.toggle('hidden', cat !== 'session');
    if (cat === 'sharing') refreshShareStatus();
}

export async function openSettingsModal() {
    const dlg = document.getElementById('settingsModal');
    const hint = document.getElementById('settings-hint');
    if (hint) { hint.textContent = SETTINGS_HINT; hint.classList.remove('err'); }
    // Disable the Sharing category on mobile so a tunnel client can't disconnect
    // itself by mis-tapping GO PRIVATE.
    const shareNav = document.querySelector('#settingsModal .snav[data-cat="sharing"]');
    if (shareNav) {
        const mobile = isMobile();
        shareNav.disabled = mobile;
        shareNav.title = mobile ? 'Manage sharing from a desktop' : '';
    }
    settingsSelectCategory('session');
    try {
        const res = await fetch('/api/settings');
        if (res.ok) {
            const s = await res.json();
            setSel('model', s.model || '');
            setSel('advisorModel', s.advisorModel || '');
            setSel('effortLevel', s.effortLevel || '');
            document.getElementById('settings-language').value = s.language || '';
            const thinking = document.getElementById('settings-thinking');
            thinking.classList.toggle('on', !!s.alwaysThinkingEnabled);
            fdSyncToggle(thinking);
        }
    } catch (e) { /* show modal with whatever defaults are present */ }
    dlg.showModal();
}

export async function saveSettings() {
    const btn = document.getElementById('settings-save');
    const label = btn.querySelector('span');
    const payload = {
        model: getSel('model'),
        advisorModel: getSel('advisorModel'),
        effortLevel: getSel('effortLevel'),
        language: document.getElementById('settings-language').value.trim(),
        alwaysThinkingEnabled: document.getElementById('settings-thinking').classList.contains('on'),
    };
    const hint = document.getElementById('settings-hint');
    const defaultHint = hint && !hint.classList.contains('err') ? hint.textContent : SETTINGS_HINT;
    if (btn.dataset.busy) return; // guard against double-submit
    // Immediate in-flight feedback — the write + settings.json refresh can take 1-2s.
    btn.dataset.busy = '1';
    btn.classList.remove('ok', 'err');
    btn.classList.add('saving');
    label.textContent = 'SAVING…';
    try {
        const res = await fetch('/api/settings', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        if (!res.ok) {
            let msg = 'Save failed (' + res.status + ')';
            try { const j = await res.json(); if (j && j.error) msg = j.error; } catch (_) {}
            throw new Error(msg);
        }
        if (hint) { hint.textContent = defaultHint; hint.classList.remove('err'); }
        btn.classList.remove('saving');
        btn.classList.add('ok');
        label.textContent = 'SAVED ✓';
        setTimeout(() => {
            btn.classList.remove('ok');
            label.textContent = 'SAVE';
            delete btn.dataset.busy;
            document.getElementById('settingsModal').close();
        }, 900);
    } catch (e) {
        // Surface the backend's reason in the footer; leave Save usable so the
        // user can fix the value and retry. The message clears on the next edit.
        if (hint) { hint.textContent = e.message; hint.classList.add('err'); }
        btn.classList.remove('saving');
        label.textContent = 'SAVE';
        delete btn.dataset.busy;
    }
}

export function init() {
    register('open-settings', () => openSettingsModal());
    register('settings-cat', (el) => settingsSelectCategory(el.dataset.cat));
    register('settings-pick', (el) => settingsPick(el));
    register('toggle-thinking', (el) => { toggleThinking(el); clearSettingsError(); });
    register('save-settings', () => saveSettings());

    document.addEventListener('click', (e) => {
        const val = e.target.closest('#settingsModal .sel-val');
        if (val) setSelOpen(val.closest('.sel'), !val.closest('.sel').classList.contains('open'));
        document.querySelectorAll('#settingsModal .sel.open').forEach(s => {
            if (!s.contains(e.target)) setSelOpen(s, false);
        });
    });

    // Keyboard contract for .sel — ported from the kit's global keydown delegate
    // (futurism.js): Enter/Space/Down open; Up/Down move between options
    // (roving focus, options are tabindex=-1 so Tab skips them while closed);
    // Enter/Space picks the focused option; Escape/Tab close.
    document.addEventListener('keydown', (e) => {
        const sel = e.target.closest && e.target.closest('#settingsModal .sel');
        if (!sel) return;
        const opts = Array.from(sel.querySelectorAll('.sel-opt'));
        const open = sel.classList.contains('open');
        const i = opts.indexOf(e.target);
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (!open) setSelOpen(sel, true);
            else if (i < opts.length - 1) opts[i + 1].focus();
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (i > 0) opts[i - 1].focus();
        } else if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            if (!open) setSelOpen(sel, true);
            else if (i > -1) settingsPick(opts[i]);
        } else if (e.key === 'Escape') {
            if (open) { e.preventDefault(); setSelOpen(sel, false); }
        } else if (e.key === 'Tab') {
            if (open) setSelOpen(sel, false);
        }
    });

    // Clear a stale save error as soon as the user edits the language.
    document.getElementById('settings-language')?.addEventListener('input', clearSettingsError);
}
