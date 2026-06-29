// Tests for syncTerminalBgVar / theme plumbing in terminal.js.
// terminal.js is a non-module browser script: it runs top-level code against
// browser globals and defines functions in global scope. We load its source
// into a node:vm context populated with minimal DOM/browser stubs, then assert
// on the globals it defines.
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import vm from 'node:vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const SRC = readFileSync(join(__dirname, 'terminal.js'), 'utf8');

// Build a fresh stub environment and evaluate terminal.js into it.
// Returns the vm context, which holds every global the script defines
// (syncTerminalBgVar, getTerminalTheme, terminalThemes, TerminalManager, ...).
function loadTerminal() {
    const setProps = {};   // captured documentElement.style.setProperty calls
    const attrs = {};      // captured documentElement attributes

    const documentElement = {
        getAttribute: (name) => (name in attrs ? attrs[name] : null),
        setAttribute: (name, value) => { attrs[name] = value; },
        style: {
            setProperty: (name, value) => { setProps[name] = value; },
        },
        scrollTop: 0,
    };

    const noopListenerTarget = { addEventListener() {}, removeEventListener() {} };

    const document = {
        documentElement,
        body: { style: {}, appendChild() {}, removeChild() {} },
        activeElement: null,
        addEventListener() {},
        getElementById: () => null,
        querySelector: () => null,
        createElement: () => ({ style: {}, select() {}, value: '' }),
        execCommand() {},
    };

    // MutationObserver is referenced at top level (new MutationObserver(...)).
    class MutationObserver {
        constructor(cb) { this.cb = cb; }
        observe() {}
        disconnect() {}
    }

    const sandbox = {
        document,
        window: noopListenerTarget,
        navigator: {},
        location: { protocol: 'http:', host: 'localhost' },
        MutationObserver,
        WebSocket: function WebSocket() {},
        // Used inside create() but never reached by these tests; define so the
        // top-level script body parses/runs without ReferenceError if touched.
        Terminal: function Terminal() {},
        FitAddon: { FitAddon: function () {} },
        WebLinksAddon: { WebLinksAddon: function () {} },
        TextEncoder,
        performance: { now: () => 0 },
        requestAnimationFrame: () => 0,
        cancelAnimationFrame: () => {},
        setTimeout: () => 0,
        clearTimeout: () => {},
        isMobile: () => false,   // defined in views.js in the browser
        console,
    };
    sandbox.globalThis = sandbox;

    const context = vm.createContext(sandbox);
    // const/function-declared top-level bindings are lexically scoped in a vm
    // script and do not become globalThis properties. Append a shim that lifts
    // the symbols under test onto globalThis so the test can reach them.
    const shim = '\n;globalThis.__exports = { syncTerminalBgVar, getTerminalTheme, terminalThemes, TerminalManager };\n';
    vm.runInContext(SRC + shim, context, { filename: 'terminal.js' });
    Object.assign(context, context.__exports);

    // Expose the capture maps for assertions.
    context.__setProps = setProps;
    context.__attrs = attrs;
    return context;
}

let ctx;
beforeEach(() => { ctx = loadTerminal(); });

test('syncTerminalBgVar sets both --terminal-bg and --terminal-fg for dark theme', () => {
    ctx.__attrs['data-theme'] = 'dark';
    ctx.syncTerminalBgVar();
    assert.equal(ctx.__setProps['--terminal-bg'], ctx.terminalThemes.dark.background);
    assert.equal(ctx.__setProps['--terminal-fg'], ctx.terminalThemes.dark.foreground);
});

test('syncTerminalBgVar sets --terminal-fg from the foreground (not background) color', () => {
    ctx.__attrs['data-theme'] = 'dark';
    ctx.syncTerminalBgVar();
    // Regression guard for the change: fg must come from foreground, and the
    // two vars must be distinct values.
    assert.equal(ctx.__setProps['--terminal-fg'], '#c9d1d9');
    assert.notEqual(ctx.__setProps['--terminal-fg'], ctx.__setProps['--terminal-bg']);
});

test('syncTerminalBgVar sets --terminal-fg for the light theme', () => {
    ctx.__attrs['data-theme'] = 'light';
    ctx.syncTerminalBgVar();
    assert.equal(ctx.__setProps['--terminal-bg'], ctx.terminalThemes.light.background);
    assert.equal(ctx.__setProps['--terminal-fg'], ctx.terminalThemes.light.foreground);
});

test('syncTerminalBgVar falls back to the dark theme when data-theme is absent', () => {
    // no data-theme attribute set
    ctx.syncTerminalBgVar();
    assert.equal(ctx.__setProps['--terminal-bg'], ctx.terminalThemes.dark.background);
    assert.equal(ctx.__setProps['--terminal-fg'], ctx.terminalThemes.dark.foreground);
});

test('syncTerminalBgVar falls back to dark for an unknown theme name', () => {
    ctx.__attrs['data-theme'] = 'no-such-theme';
    ctx.syncTerminalBgVar();
    assert.equal(ctx.__setProps['--terminal-fg'], ctx.terminalThemes.dark.foreground);
});

test('syncTerminalBgVar sets data-theme-base=dark for a dark-base theme', () => {
    ctx.__attrs['data-theme'] = 'dark';
    ctx.syncTerminalBgVar();
    assert.equal(ctx.__attrs['data-theme-base'], 'dark');
});

test('syncTerminalBgVar sets data-theme-base=light for each light-base theme', () => {
    for (const name of ['light', 'cupcake', 'autumn']) {
        const c = loadTerminal();
        c.__attrs['data-theme'] = name;
        c.syncTerminalBgVar();
        assert.equal(c.__attrs['data-theme-base'], 'light', `theme ${name}`);
    }
});

test('getTerminalTheme returns the matching theme object and the fg used by syncTerminalBgVar', () => {
    ctx.__attrs['data-theme'] = 'light';
    const theme = ctx.getTerminalTheme();
    assert.equal(theme, ctx.terminalThemes.light);
    ctx.syncTerminalBgVar();
    assert.equal(ctx.__setProps['--terminal-fg'], theme.foreground);
});

test('rethemeAll syncs the CSS vars including --terminal-fg', () => {
    ctx.__attrs['data-theme'] = 'light';
    // No terminal instances — rethemeAll still calls syncTerminalBgVar().
    ctx.TerminalManager.rethemeAll();
    assert.equal(ctx.__setProps['--terminal-fg'], ctx.terminalThemes.light.foreground);
    assert.equal(ctx.__setProps['--terminal-bg'], ctx.terminalThemes.light.background);
});
