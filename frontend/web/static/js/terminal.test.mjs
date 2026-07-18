// Tests for syncTerminalBgVar / theme plumbing in terminal.js.
// terminal.js is now an ES module: import it once, then drive each test against
// a fresh fake document installed on globalThis so its documentElement style /
// attribute writes are captured for assertions.
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

const { syncTerminalBgVar, getTerminalTheme, terminalThemes, TerminalManager } = await import('./terminal.js');

// Install a fresh capturing document on globalThis and return the exports under
// test plus the capture maps (documentElement.style.setProperty / attributes).
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

    globalThis.document = {
        documentElement,
        body: { style: {}, appendChild() {}, removeChild() {} },
        activeElement: null,
        addEventListener() {},
        getElementById: () => null,
        querySelector: () => null,
        createElement: () => ({ style: {}, select() {}, value: '' }),
        execCommand() {},
    };
    globalThis.window = { addEventListener() {}, removeEventListener() {} };

    return { syncTerminalBgVar, getTerminalTheme, terminalThemes, TerminalManager, __setProps: setProps, __attrs: attrs };
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
