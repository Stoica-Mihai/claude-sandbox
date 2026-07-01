// Theme: light/dark toggle + accent picker, persisted to localStorage.

var ACCENTS = [
    { name: 'Red',    dark: '#ff4d33', light: '#d22f1a' },
    { name: 'Amber',  dark: '#ffb02e', light: '#c97a00' },
    { name: 'Lime',   dark: '#9ae600', light: '#5d8a00' },
    { name: 'Cyan',   dark: '#2ee6d6', light: '#0a8f86' },
    { name: 'Blue',   dark: '#4d8bff', light: '#1f5fd6' },
    { name: 'Violet', dark: '#b06bff', light: '#7a3fd6' },
    { name: 'Pink',   dark: '#ff5fae', light: '#d62f86' },
];

function currentBaseIsDark() {
    return document.documentElement.getAttribute('data-theme') !== 'light';
}

// Relative luminance (WCAG) of a #rgb/#rrggbb color, 0..1. Ported from the kit's
// futurism.js so the accent picker derives --on-accent the way fdAccent() does.
function fdLuminance(hex) {
    hex = String(hex).replace('#', '');
    if (hex.length === 3) hex = hex.replace(/./g, '$&$&');
    var v = [0, 2, 4].map(function (i) {
        var c = parseInt(hex.substr(i, 2), 16) / 255;
        return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2];
}

// Pick the kit's cream or near-black foreground — whichever contrasts better on
// col — so text/icons on an accent fill stay legible for any picked accent.
function fdOnAccent(col) {
    var cream = '#efe9dc', ink = '#16140f', L = fdLuminance(col);
    function ratio(a, b) { return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05); }
    return ratio(L, fdLuminance(cream)) >= ratio(L, fdLuminance(ink)) ? cream : ink;
}

var savedAccent = localStorage.getItem('accent') || 'Red';
var curAccent = ACCENTS.find(function(a) { return a.name === savedAccent; }) || ACCENTS[0];

function renderAccents() {
    var dark = currentBaseIsDark();
    var pop = document.getElementById('accpop');
    var trig = document.getElementById('acctrig');
    if (trig) trig.style.background = dark ? curAccent.dark : curAccent.light;
    if (!pop) return;
    pop.innerHTML = '';
    ACCENTS.forEach(function(a) {
        var s = document.createElement('button');
        var selected = a.name === curAccent.name;
        s.className = 'acc' + (selected ? ' on' : '');
        s.style.background = dark ? a.dark : a.light;
        s.title = a.name;
        s.setAttribute('aria-label', a.name);
        s.setAttribute('aria-pressed', selected ? 'true' : 'false');
        s.onclick = function() {
            curAccent = a;
            applyAccent();
            closeAccentPicker();
        };
        pop.appendChild(s);
    });
    syncSwatchFocus();
}

// Swatches default to real <button>s (natively tabbable), so without this
// they'd stay in Tab order even while the popover is closed/invisible
// (transform:scaleY(0) blocks pointer/visual access but not keyboard focus).
// Pull them out of the tab order when closed, same as .sel-opt's tabindex=-1.
// Ported from the kit's fdAccent(); our setAccentPickerOpen() is the single
// funnel every open/close path already goes through (toggle, outside-click,
// Escape), so — unlike the kit's version — no MutationObserver is needed to
// catch a bypass path; there isn't one.
function syncSwatchFocus() {
    var pick = document.getElementById('accpick');
    var pop = document.getElementById('accpop');
    if (!pick || !pop) return;
    var open = pick.classList.contains('open');
    Array.prototype.slice.call(pop.querySelectorAll('.acc')).forEach(function(s) {
        s.tabIndex = open ? 0 : -1;
    });
}

// Open/close the accent popover, keeping aria-expanded in sync (mirrors the
// kit's fdSelOpen contract for .sel, ported since futurism.js isn't vendored).
function toggleAccentPicker() {
    var pick = document.getElementById('accpick');
    if (!pick) return;
    setAccentPickerOpen(!pick.classList.contains('open'));
}
function setAccentPickerOpen(open) {
    var pick = document.getElementById('accpick');
    var trig = document.getElementById('acctrig');
    if (!pick) return;
    pick.classList.toggle('open', open);
    if (trig) trig.setAttribute('aria-expanded', open ? 'true' : 'false');
    syncSwatchFocus();
}
function closeAccentPicker() {
    setAccentPickerOpen(false);
    var trig = document.getElementById('acctrig');
    if (trig) trig.focus();
}

function applyAccent() {
    var dark = currentBaseIsDark();
    var col = dark ? curAccent.dark : curAccent.light;
    var r = document.documentElement.style;
    r.setProperty('--accent', col);
    // dark base uses accent as the offset-shadow; light base keeps ink
    r.setProperty('--shadow', dark ? col : '#1a1714');
    // Re-derive the paired on-accent foreground for the picked color, else text/
    // icons on an accent fill keep the red-paired token and go low-contrast.
    r.setProperty('--on-accent', fdOnAccent(col));
    localStorage.setItem('accent', curAccent.name);
    renderAccents();
}

// Sync aria-checked (role=switch) from the .on class — a real <button> handles
// its own Enter/Space activation, so only the ARIA state needs wiring by hand.
function fdSyncToggle(el) {
    if (el) el.setAttribute('aria-checked', el.classList.contains('on') ? 'true' : 'false');
}

function toggleThinking(el) {
    el.classList.toggle('on');
    fdSyncToggle(el);
}

function flipTheme() {
    var r = document.documentElement;
    var dark = r.getAttribute('data-theme') === 'dark';
    var next = dark ? 'light' : 'dark';
    r.setAttribute('data-theme', next);
    r.setAttribute('data-theme-base', next === 'light' ? 'light' : 'dark');
    localStorage.setItem('theme', next);
    var t = document.getElementById('themeToggle');
    if (t) { t.classList.toggle('on', !dark); fdSyncToggle(t); }
    applyAccent();
    if (typeof syncTerminalBgVar === 'function') {
        syncTerminalBgVar();
    }
    if (typeof TerminalManager !== 'undefined') {
        TerminalManager.rethemeAll();
    }
}

document.addEventListener('click', function(e) {
    var p = document.getElementById('accpick');
    if (p && !p.contains(e.target)) setAccentPickerOpen(false);
});

// Escape closes the accent popover and returns focus to its trigger — ported
// from the kit's global keydown delegate (futurism.js), scoped to .accpick.open.
document.addEventListener('keydown', function(e) {
    if (e.key !== 'Escape') return;
    var p = document.getElementById('accpick');
    if (p && p.classList.contains('open')) closeAccentPicker();
});

// A pressed .btn depresses (kit :active drops translate + shrinks the offset
// shadow), which slides it out from under a press begun on the hovered top
// edge. Capture the pointer so the click still retargets to the button even
// though it moved — ported from futurism.js (not vendored; we drive our own
// JS), since this is a hard CSS/JS pairing, not progressive enhancement.
document.addEventListener('pointerdown', function(e) {
    var b = e.target && e.target.closest && e.target.closest('.btn');
    if (b && e.pointerId != null && b.setPointerCapture) {
        try { b.setPointerCapture(e.pointerId); } catch (_) {}
    }
});

(function() {
    var savedTheme = localStorage.getItem('theme') || 'dark';
    var theme = savedTheme === 'light' ? 'light' : 'dark';
    var root = document.documentElement;
    root.setAttribute('data-theme', theme);
    root.setAttribute('data-theme-base', theme === 'light' ? 'light' : 'dark');

    var t = document.getElementById('themeToggle');
    if (t) { t.classList.toggle('on', theme === 'dark'); fdSyncToggle(t); }

    applyAccent();

    if (typeof syncTerminalBgVar === 'function') {
        syncTerminalBgVar();
    }
})();
