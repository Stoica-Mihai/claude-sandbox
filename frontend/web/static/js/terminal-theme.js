// Terminal color themes and the CSS-var/theme plumbing that keeps app.css in
// sync with the active xterm theme.

// One theme per app theme.
export const terminalThemes = {
    dark: {
        background: '#0d1117',
        foreground: '#c9d1d9',
        cursor: '#58a6ff',
        selectionBackground: '#264f78',
        black: '#0d1117',
        red: '#ff7b72',
        green: '#3fb950',
        yellow: '#d29922',
        blue: '#58a6ff',
        magenta: '#bc8cff',
        cyan: '#39d353',
        white: '#c9d1d9',
        brightBlack: '#484f58',
        brightRed: '#ffa198',
        brightGreen: '#56d364',
        brightYellow: '#e3b341',
        brightBlue: '#79c0ff',
        brightMagenta: '#d2a8ff',
        brightCyan: '#56d364',
        brightWhite: '#f0f6fc',
    },
    light: {
        background: '#ffffff',
        foreground: '#24292f',
        cursor: '#0969da',
        selectionBackground: '#b4d7ff',
        black: '#24292e',
        red: '#d1242f',
        green: '#1a7f37',
        yellow: '#9a6700',
        blue: '#0969da',
        magenta: '#8250df',
        cyan: '#1b7c83',
        white: '#6e7781',
        brightBlack: '#57606a',
        brightRed: '#cf222e',
        brightGreen: '#2da44e',
        brightYellow: '#bf8700',
        brightBlue: '#218bff',
        brightMagenta: '#a475f9',
        brightCyan: '#3192aa',
        brightWhite: '#8c959f',
    },
};

export function getTerminalTheme() {
    const appTheme = document.documentElement.getAttribute('data-theme') || 'dark';
    return terminalThemes[appTheme] || terminalThemes.dark;
}

// Mirror the active terminal theme's colors into CSS vars, and set data-theme-base
// so app.css can key light/dark overrides off the terminal's base.
export function syncTerminalBgVar() {
    const theme = getTerminalTheme();
    document.documentElement.style.setProperty('--terminal-bg', theme.background);
    document.documentElement.style.setProperty('--terminal-fg', theme.foreground);
    // The app theme is a binary light/dark toggle (theme.js); base follows it.
    const isLight = document.documentElement.getAttribute('data-theme') === 'light';
    document.documentElement.setAttribute('data-theme-base', isLight ? 'light' : 'dark');
}
