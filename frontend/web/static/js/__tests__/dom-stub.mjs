// Minimal DOM stub: just enough surface for views.js to load and for the
// class/text state-toggle paths under test to be driven and asserted.

class ClassList {
    constructor() { this._set = new Set(); }
    add(...names) { names.forEach(n => this._set.add(n)); }
    remove(...names) { names.forEach(n => this._set.delete(n)); }
    contains(name) { return this._set.has(name); }
    toggle(name, force) {
        const want = force === undefined ? !this._set.has(name) : !!force;
        if (want) this._set.add(name); else this._set.delete(name);
        return want;
    }
    get value() { return [...this._set].join(' '); }
    toString() { return this.value; }
}

class FakeElement {
    constructor(tag = 'div') {
        this.tagName = (tag || 'div').toUpperCase();
        this.classList = new ClassList();
        this.style = makeStyle();
        this.dataset = {};
        this.children = [];
        this.attributes = {};
        this._listeners = {};
        this._textContent = '';
        this._innerHTML = '';
        this.id = '';
        this.value = '';
        this.disabled = false;
        this.parentNode = null;
        this.onclick = null;
        this.onauxclick = null;
    }
    get className() { return this.classList.value; }
    set className(v) {
        this.classList = new ClassList();
        String(v).split(/\s+/).filter(Boolean).forEach(c => this.classList.add(c));
    }
    set textContent(v) { this._textContent = String(v); this.children = []; }
    // Real DOM concatenates all descendant text; app code here never mixes
    // literal text with children on the same node (always clears one before
    // appending the other), so children-present implies "reflect them".
    get textContent() { return this.children.length ? this.children.map(c => c.textContent).join('') : this._textContent; }
    set innerHTML(v) { this._innerHTML = String(v); }
    get innerHTML() { return this._innerHTML; }
    appendChild(child) { this.children.push(child); child.parentNode = this; return child; }
    remove() {
        if (this.parentNode) {
            const i = this.parentNode.children.indexOf(this);
            if (i >= 0) this.parentNode.children.splice(i, 1);
            this.parentNode = null;
        }
    }
    addEventListener(type, fn) {
        (this._listeners[type] = this._listeners[type] || []).push(fn);
    }
    removeEventListener(type, fn) {
        const arr = this._listeners[type];
        if (arr) this._listeners[type] = arr.filter(f => f !== fn);
    }
    dispatch(type, event) {
        (this._listeners[type] || []).forEach(fn => fn(event));
    }
    querySelector(sel) { return matchDescendants(this, sel)[0] || null; }
    querySelectorAll(sel) { return matchDescendants(this, sel); }
    // Match a simple selector: .class, [attr], [attr="val"], or a tag name.
    matches(sel) {
        sel = String(sel).trim();
        if (sel.startsWith('.')) return this.classList.contains(sel.slice(1));
        if (sel.startsWith('[')) {
            const m = sel.match(/^\[([^\]=]+)(?:="([^"]*)")?\]$/);
            if (!m) return false;
            let val;
            if (m[1].startsWith('data-')) {
                const key = m[1].slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
                val = this.dataset[key];
            } else {
                val = this.attributes[m[1]];
            }
            return m[2] === undefined ? val != null : val === m[2];
        }
        return this.tagName === sel.toUpperCase();
    }
    // Walk self → ancestors for the first element matching sel.
    closest(sel) {
        let el = this;
        while (el) {
            if (el.matches && el.matches(sel)) return el;
            el = el.parentNode;
        }
        return null;
    }
    focus() {}
    select() {}
    setAttribute(k, v) { this.attributes[k] = String(v); }
    getAttribute(k) { return this.attributes[k] ?? null; }
    showModal() { this._open = true; }
    close() { this._open = false; }
    scrollToBottom() {}
    get scrollHeight() { return 100; }
}

// Match descendants of `root` against a comma-separated list of simple
// class selectors (e.g. ".actitle, .arow-wrap"). Class selectors only —
// enough for the dir-picker history/delete paths under test.
function matchDescendants(root, sel) {
    const classes = String(sel).split(',')
        .map(s => s.trim())
        .filter(s => s.startsWith('.'))
        .map(s => s.slice(1));
    if (classes.length === 0) return [];
    const out = [];
    const walk = (el) => {
        (el.children || []).forEach(child => {
            if (classes.some(c => child.classList && child.classList.contains(c))) {
                out.push(child);
            }
            walk(child);
        });
    };
    walk(root);
    return out;
}

function makeStyle() {
    // Proxy so views.js can write arbitrary style props without us declaring them.
    // Also implements the CSSStyleDeclaration methods (setProperty/removeProperty/
    // getPropertyValue) real code uses for custom properties (e.g. --row-act-h),
    // since dot-notation can't address a '--foo' key.
    const methods = {
        setProperty(t) { return (name, value) => { t[name] = value; }; },
        removeProperty(t) { return (name) => { delete t[name]; }; },
        getPropertyValue(t) { return (name) => (name in t ? t[name] : ''); },
    };
    return new Proxy({ cssText: '' }, {
        get(t, p) { return p in methods ? methods[p](t) : (p in t ? t[p] : ''); },
        set(t, p, v) { t[p] = v; return true; },
    });
}

class FakeDocument {
    constructor() {
        this._byId = new Map();
        this._listeners = {};
        this.documentElement = new FakeElement('html');
        this.body = new FakeElement('body');
        this.activeElement = new FakeElement('body');
    }
    register(id, el) { el.id = id; this._byId.set(id, el); return el; }
    getElementById(id) { return this._byId.get(id) || null; }
    createElement(tag) { return new FakeElement(tag); }
    querySelector() { return null; }
    querySelectorAll() { return []; }
    addEventListener(type, fn) {
        (this._listeners[type] = this._listeners[type] || []).push(fn);
    }
    dispatch(type, event) {
        (this._listeners[type] || []).forEach(fn => fn(event));
    }
}

export { ClassList, FakeElement, FakeDocument, makeStyle };
