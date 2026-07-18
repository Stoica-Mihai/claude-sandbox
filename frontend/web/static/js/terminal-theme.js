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
        background: '#1e1e2e',
        foreground: '#cdd6f4',
        cursor: '#0969da',
        selectionBackground: '#313244',
        black: '#1e1e2e',
        red: '#f38ba8',
        green: '#a6e3a1',
        yellow: '#f9e2af',
        blue: '#89b4fa',
        magenta: '#cba6f7',
        cyan: '#94e2d5',
        white: '#cdd6f4',
        brightBlack: '#585b70',
        brightRed: '#f38ba8',
        brightGreen: '#a6e3a1',
        brightYellow: '#f9e2af',
        brightBlue: '#89b4fa',
        brightMagenta: '#cba6f7',
        brightCyan: '#94e2d5',
        brightWhite: '#f5f5f5',
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
    const lightThemes = ['light', 'cupcake', 'autumn'];
    const isLight = lightThemes.includes(document.documentElement.getAttribute('data-theme'));
    document.documentElement.setAttribute('data-theme-base', isLight ? 'light' : 'dark');
}
