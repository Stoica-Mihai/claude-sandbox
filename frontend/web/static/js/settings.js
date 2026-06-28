// Settings editor: GET/PUT the whitelisted preference subset of container-settings.json.
// Applies to NEW sessions (claude reads settings at spawn).

// Custom select: open on value click, pick sets value + closes; click-outside closes.
// An option's value is its data-value when present (label != value, e.g. the
// advisor's "Opus 4.8" -> "claude-opus-4-8"), otherwise its label text.
function optValue(o) { return o.dataset.value !== undefined ? o.dataset.value : o.textContent; }

function settingsPick(opt) {
    const sel = opt.closest('.sel');
    sel.querySelectorAll('.sel-opt').forEach(o => o.classList.remove('sel-on'));
    opt.classList.add('sel-on');
    sel.querySelector('.sel-cur').textContent = opt.textContent;
    sel.dataset.value = optValue(opt);
    sel.classList.remove('open');
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
    try {
        const res = await fetch('/api/settings', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error('save failed (' + res.status + ')');
        btn.classList.add('ok');
        label.textContent = 'SAVED ✓';
        setTimeout(() => {
            btn.classList.remove('ok');
            label.textContent = 'SAVE';
            document.getElementById('settingsModal').close();
        }, 1000);
    } catch (e) {
        btn.classList.add('err');
        label.textContent = 'FAILED — RETRY';
        setTimeout(() => { btn.classList.remove('err'); label.textContent = 'SAVE'; }, 2000);
    }
}
