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
        s.className = 'acc' + (a.name === curAccent.name ? ' on' : '');
        s.style.background = dark ? a.dark : a.light;
        s.title = a.name;
        s.onclick = function() {
            curAccent = a;
            applyAccent();
            var pick = document.getElementById('accpick');
            if (pick) pick.classList.remove('open');
        };
        pop.appendChild(s);
    });
}

function applyAccent() {
    var dark = currentBaseIsDark();
    var col = dark ? curAccent.dark : curAccent.light;
    var r = document.documentElement.style;
    r.setProperty('--accent', col);
    // dark base uses accent as the offset-shadow; light base keeps ink
    r.setProperty('--shadow', dark ? col : '#1a1714');
    localStorage.setItem('accent', curAccent.name);
    renderAccents();
}

function flipTheme() {
    var r = document.documentElement;
    var dark = r.getAttribute('data-theme') === 'dark';
    var next = dark ? 'light' : 'dark';
    r.setAttribute('data-theme', next);
    r.setAttribute('data-theme-base', next === 'light' ? 'light' : 'dark');
    localStorage.setItem('theme', next);
    var t = document.getElementById('themeToggle');
    if (t) t.classList.toggle('on', !dark);
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
    if (p && !p.contains(e.target)) p.classList.remove('open');
});

(function() {
    var savedTheme = localStorage.getItem('theme') || 'dark';
    var theme = savedTheme === 'light' ? 'light' : 'dark';
    var root = document.documentElement;
    root.setAttribute('data-theme', theme);
    root.setAttribute('data-theme-base', theme === 'light' ? 'light' : 'dark');

    var t = document.getElementById('themeToggle');
    if (t) t.classList.toggle('on', theme === 'dark');

    applyAccent();

    if (typeof syncTerminalBgVar === 'function') {
        syncTerminalBgVar();
    }
})();
