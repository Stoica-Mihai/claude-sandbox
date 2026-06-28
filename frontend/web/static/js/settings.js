// Settings editor: GET/PUT the whitelisted preference subset of container-settings.json.
// Applies to NEW sessions (claude reads settings at spawn).

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

function settingsPick(opt) {
    const sel = opt.closest('.sel');
    sel.querySelectorAll('.sel-opt').forEach(o => o.classList.remove('sel-on'));
    opt.classList.add('sel-on');
    sel.querySelector('.sel-cur').textContent = opt.textContent;
    sel.dataset.value = optValue(opt);
    sel.classList.remove('open');
    clearSettingsError();
}
document.addEventListener('click', (e) => {
    const val = e.target.closest('#settingsModal .sel-val');
    if (val) val.closest('.sel').classList.toggle('open');
    document.querySelectorAll('#settingsModal .sel.open').forEach(s => {
        if (!s.contains(e.target)) s.classList.remove('open');
    });
});

// Set a .sel to a value (matched against each option's data-value/label),
// showing that option's label and storing the value on the element.
function setSel(field, value) {
    const sel = document.querySelector(`#settingsModal .sel[data-field="${field}"]`);
    if (!sel) return;
    value = value || '';
    sel.dataset.value = value;
    let match = null;
    sel.querySelectorAll('.sel-opt').forEach(o => {
        const on = optValue(o) === value;
        o.classList.toggle('sel-on', on);
        if (on) match = o;
    });
    sel.querySelector('.sel-cur').textContent = match ? match.textContent : (value || '—');
}

function getSel(field) {
    const sel = document.querySelector(`#settingsModal .sel[data-field="${field}"]`);
    return sel ? (sel.dataset.value || '') : '';
}

async function openSettingsModal() {
    const dlg = document.getElementById('settingsModal');
    const hint = document.getElementById('settings-hint');
    if (hint) { hint.textContent = SETTINGS_HINT; hint.classList.remove('err'); }
    try {
        const res = await fetch('/api/settings');
        if (res.ok) {
            const s = await res.json();
            setSel('model', s.model || '');
            setSel('advisorModel', s.advisorModel || '');
            setSel('effortLevel', s.effortLevel || '');
            document.getElementById('settings-language').value = s.language || '';
            document.getElementById('settings-thinking').classList.toggle('on', !!s.alwaysThinkingEnabled);
        }
    } catch (e) { /* show modal with whatever defaults are present */ }
    dlg.showModal();
}

async function saveSettings() {
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

// Clear a stale save error as soon as the user edits the language or toggle.
document.getElementById('settings-language')?.addEventListener('input', clearSettingsError);
document.getElementById('settings-thinking')?.addEventListener('click', clearSettingsError);
