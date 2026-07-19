// Theme: light/dark toggle + accent picker, persisted to localStorage.

import { syncTerminalBgVar, TerminalManager } from './terminal.js';
import { register } from './actions.js';
import { sendJSON } from './ui-utils.js';

// Accent palette injected by layout.html from shared/enums.go (single source
// with the backend's name validation).
let ACCENTS = (typeof window !== 'undefined' && window.ACCENTS) || [];
let curAccent = null;

function currentBaseIsDark() {
    return document.documentElement.getAttribute('data-theme') !== 'light';
}

// Relative luminance (WCAG) of a #rgb/#rrggbb color, 0..1. Ported from the kit's
// futurism.js so the accent picker derives --on-accent the way fdAccent() does.
export function fdLuminance(hex) {
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
export function fdOnAccent(col) {
    var cream = '#efe9dc', ink = '#16140f', L = fdLuminance(col);
    function ratio(a, b) { return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05); }
    return ratio(L, fdLuminance(cream)) >= ratio(L, fdLuminance(ink)) ? cream : ink;
}

// Renders the accent swatches inline into the Appearance settings panel.
export function renderAccents() {
    var dark = currentBaseIsDark();
    var row = document.getElementById('accpop');
    if (!row) return;
    row.innerHTML = '';
    ACCENTS.forEach(function(a) {
        var s = document.createElement('button');
        var selected = a.name === curAccent.name;
        s.type = 'button';
        s.className = 'acc' + (selected ? ' on' : '');
        s.style.background = dark ? a.dark : a.light;
        s.title = a.name;
        s.setAttribute('aria-label', a.name);
        s.setAttribute('aria-pressed', selected ? 'true' : 'false');
        s.onclick = function() {
            curAccent = a;
            applyAccent();
            saveUIPrefs();
        };
        row.appendChild(s);
    });
}

export function applyAccent() {
    if (!curAccent) return;
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
export function fdSyncToggle(el) {
    if (el) el.setAttribute('aria-checked', el.classList.contains('on') ? 'true' : 'false');
}

export function toggleThinking(el) {
    el.classList.toggle('on');
    fdSyncToggle(el);
}

// Apply a theme value everywhere (attributes, toggle, accent, terminals) and
// cache it locally. Shared by flipTheme (user action) and loadUIPrefs (sync).
export function setTheme(next) {
    var r = document.documentElement;
    r.setAttribute('data-theme', next);
    r.setAttribute('data-theme-base', next === 'light' ? 'light' : 'dark');
    localStorage.setItem('theme', next);
    var t = document.getElementById('themeToggle');
    if (t) { t.classList.toggle('on', next === 'dark'); fdSyncToggle(t); }
    applyAccent();
    syncTerminalBgVar();
    TerminalManager.rethemeAll();
}

export function flipTheme() {
    var next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    setTheme(next);
    saveUIPrefs();
}

// Accent + theme sync server-side (dashboard-ui.json) so they carry across
// devices. localStorage stays the instant-paint cache; the server is the source
// of truth, reconciled on load. Single-tenant dashboard, so last write wins.
export function saveUIPrefs() {
    var theme = document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark';
    sendJSON('/api/ui-prefs', 'PUT', { accent: curAccent.name, theme: theme }).catch(function() {});
}

export function loadUIPrefs() {
    fetch('/api/ui-prefs').then(function(r) {
        return r.ok ? r.json() : null;
    }).then(function(p) {
        if (!p) return;
        if ((p.theme === 'light' || p.theme === 'dark') &&
            document.documentElement.getAttribute('data-theme') !== p.theme) {
            setTheme(p.theme);
        }
        var a = p.accent && ACCENTS.find(function(x) { return x.name === p.accent; });
        if (a && a.name !== curAccent.name) {
            curAccent = a;
            applyAccent();
        }
    }).catch(function() {});
}

export function init() {
    ACCENTS = (typeof window !== 'undefined' && window.ACCENTS) || [];
    var savedAccent = localStorage.getItem('accent') || 'Red';
    curAccent = ACCENTS.find(function(a) { return a.name === savedAccent; }) || ACCENTS[0];

    register('flip-theme', () => flipTheme());

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

    var savedTheme = localStorage.getItem('theme') || 'dark';
    var theme = savedTheme === 'light' ? 'light' : 'dark';
    var root = document.documentElement;
    root.setAttribute('data-theme', theme);
    root.setAttribute('data-theme-base', theme === 'light' ? 'light' : 'dark');

    var t = document.getElementById('themeToggle');
    if (t) { t.classList.toggle('on', theme === 'dark'); fdSyncToggle(t); }

    applyAccent();
    syncTerminalBgVar();

    // Reconcile from the server (source of truth) after the instant local paint.
    loadUIPrefs();
}
