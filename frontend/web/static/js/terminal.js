// Terminal surface: TerminalView owns one xterm.js instance and its concerns
// (theme, socket relay, clipboard, mobile touch — each in a sibling
// terminal-*.js module); TerminalManager is the factory + registry keyed by
// terminalId, exposing create/get/destroy/resize/rethemeAll for the callers.

import { isMobile } from './ui-utils.js';
import { getTerminalTheme, syncTerminalBgVar, terminalThemes } from './terminal-theme.js';
import { SessionSocket } from './session-socket.js';
import { ANSI_RED, ANSI_GRAY, ANSI_RESET } from './terminal-ansi.js';
import { wireClipboard } from './terminal-clipboard.js';
import { wireTouchScroll } from './terminal-touch.js';
import { wsControl } from './protocol.js';

// Re-export the theme surface so existing importers (theme.js, tests) keep
// importing it from terminal.js.
export { terminalThemes, getTerminalTheme, syncTerminalBgVar };

// One encoder for every keystroke (allocating one per input is pure waste).
const textEncoder = new TextEncoder();

// TerminalView owns a single xterm.js terminal + its SessionSocket relay. The
// constructor builds the terminal, opens it into containerEl, wires clipboard/
// touch/input, and connects the socket; this.destroyed guards async callbacks
// against a disposed view.
export class TerminalView {
    constructor(terminalId, containerEl) {
        this.terminalId = terminalId;
        this.containerId = containerEl.id;
        this.needsRefresh = false; // set when another viewer resized us; display is garbled until next input
        this.destroyed = false;

        const mobile = isMobile();
        this.fitAddon = new FitAddon.FitAddon();
        this.webLinksAddon = new WebLinksAddon.WebLinksAddon();
        const term = this.term = new Terminal({
            cursorBlink: true,
            rightClickSelectsWord: !mobile,
            // Smaller on mobile so more columns fit per line on a narrow screen.
            fontSize: mobile ? 12 : 14,
            fontFamily: "Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
            lineHeight: 1.15,
            theme: getTerminalTheme(),
            allowProposedApi: true,
        });

        term.loadAddon(this.fitAddon);
        term.loadAddon(this.webLinksAddon);

        // GPU-accelerated rendering (desktop only — saves battery on mobile).
        this.webglAddon = null;
        if (!mobile && typeof WebglAddon !== 'undefined') {
            try {
                const webglAddon = new WebglAddon.WebglAddon();
                webglAddon.onContextLoss(() => {
                    // Clear our ref so a later destroy() can't dispose it twice.
                    this.webglAddon?.dispose();
                    this.webglAddon = null;
                });
                term.loadAddon(webglAddon);
                this.webglAddon = webglAddon;
            } catch (e) {
                this.webglAddon = null;
            }
        }

        // Prevent the browser from intercepting keys that Claude Code needs.
        term.attachCustomKeyEventHandler((e) => {
            if (e.type !== 'keydown') return true;
            // Let Alt+key combos pass through to our shortcut handler (don't send to PTY).
            if (e.altKey && !e.ctrlKey && !e.metaKey) {
                return false;
            }
            // Block browser default for Escape, Ctrl+C, Ctrl+D, etc.
            if (e.key === 'Escape' || (e.ctrlKey && ['c', 'd', 'z', 'l'].includes(e.key))) {
                e.preventDefault();
                e.stopPropagation();
            }
            // Block xterm from sending raw \x16 for Ctrl+V — paste is handled in
            // terminal-clipboard via a capture-phase listener.
            if (e.key === 'v' && (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) {
                return false;
            }
            return true;
        });

        term.open(containerEl);

        wireClipboard(containerEl, term, terminalId, TerminalManager, mobile);
        if (mobile) {
            wireTouchScroll(containerEl, term);
        }

        // Fit after opening (needs a frame for container dimensions).
        requestAnimationFrame(() => {
            try {
                this.fitAddon.fit();
            } catch (e) { /* container may not be visible yet */ }
        });

        let scrollRafPending = false;
        const socket = this.socket = new SessionSocket(terminalId, {
            onData: (data) => {
                const buf = term.buffer.active;
                const atBottom = buf.baseY <= buf.viewportY + 5;
                term.write(data);
                if (atBottom && !scrollRafPending) {
                    scrollRafPending = true;
                    requestAnimationFrame(() => {
                        // Skip if the view was disposed before this frame ran.
                        if (!this.destroyed) term.scrollToBottom();
                        scrollRafPending = false;
                    });
                }
            },
            onControl: (msg) => {
                if (msg.type === wsControl().DEACTIVATED) this.needsRefresh = true;
            },
            onStatus: (status, info) => {
                if (status === 'open') {
                    // The server replays its full ring buffer on every (re)attach.
                    // On a resume our buffer still holds the pre-disconnect
                    // scrollback, so reset first or the replay duplicates it.
                    if (info.resumed) term.reset();
                    socket.sendResize(term.cols, term.rows);
                } else if (status === 'reconnecting') {
                    term.write(`\r\n${ANSI_GRAY}[Reconnecting... (attempt ${info.attempt})]${ANSI_RESET}`);
                } else if (status === 'connecting' && info.retry) {
                    term.write(`\r\n${ANSI_GRAY}[Retrying...]${ANSI_RESET}`);
                } else if (status === 'ended') {
                    term.write(`\r\n${ANSI_GRAY}[Session ended]${ANSI_RESET}\r\n`);
                } else if (status === 'lost') {
                    term.write(`\r\n${ANSI_RED}[Connection lost — press any key to retry]${ANSI_RESET}\r\n`);
                }
            },
        });
        socket.connect();

        // Focusing a suspended terminal takes the live view back WITHOUT typing:
        // ask the server to reactivate us and repaint (a fresh snapshot arrives).
        // This is the passive counterpart to the input-driven takeover below —
        // it injects nothing into the session.
        containerEl.addEventListener('focusin', () => {
            if (socket.status === 'open' && this.needsRefresh) {
                this.needsRefresh = false;
                socket.sendControl({ type: wsControl().REACTIVATE });
            }
        });

        // User input -> socket (binary so Go routes it to the PTY).
        term.onData((data) => {
            if (socket.status === 'lost') {
                socket.retry();
                return;
            }
            // If another viewer resized the session, clear our garbled display.
            // The input triggers the active-viewer takeover on the server, which
            // resizes the PTY back to us; the redraw arrives via broadcast.
            if (socket.status === 'open' && this.needsRefresh) {
                this.needsRefresh = false;
                term.clear();
            }
            socket.send(textEncoder.encode(data));
            // On mobile, dismiss the keyboard after Enter so output is visible.
            if (data === '\r' && isMobile()) {
                const ta = containerEl.querySelector('.xterm-helper-textarea');
                if (ta) setTimeout(() => ta.blur(), 50);
            }
        });

        term.onResize(({ cols, rows }) => {
            socket.sendResize(cols, rows);
        });
    }

    resize() {
        try {
            this.fitAddon.fit();
            this.term.scrollToBottom();
        } catch (e) { /* container may not be visible */ }
    }

    retheme(theme) {
        this.term.options.theme = theme;
    }

    destroy() {
        this.destroyed = true;
        this.socket?.close(); // intentional: no reconnect
        if (this.webglAddon) {
            this.webglAddon.dispose();
        }
        this.term.dispose();
    }
}

// TerminalManager — factory + registry for the live TerminalView instances,
// keyed by terminalId.
export const TerminalManager = {
    instances: {}, // terminalId -> TerminalView

    create(terminalId, containerEl) {
        if (this.instances[terminalId]) {
            this.destroy(terminalId);
        }
        const view = new TerminalView(terminalId, containerEl);
        this.instances[terminalId] = view;
        return view;
    },

    destroy(terminalId) {
        const view = this.instances[terminalId];
        if (!view) return;
        view.destroy();
        delete this.instances[terminalId];
    },

    resize(terminalId) {
        this.instances[terminalId]?.resize();
    },

    resizeAll() {
        for (const view of Object.values(this.instances)) {
            view.resize();
        }
    },

    get(terminalId) {
        return this.instances[terminalId] || null;
    },

    rethemeAll() {
        const theme = getTerminalTheme();
        syncTerminalBgVar();
        for (const view of Object.values(this.instances)) {
            view.retheme(theme);
        }
    },
};

export function init() {
    // Block <dialog> cancel (Escape) while the terminal is focused.
    document.addEventListener('cancel', (e) => {
        if (document.activeElement?.classList?.contains('xterm-helper-textarea')) {
            e.preventDefault();
        }
    }, true);

    // Debounced window resize.
    let resizeTimeout;
    window.addEventListener('resize', () => {
        clearTimeout(resizeTimeout);
        resizeTimeout = setTimeout(() => TerminalManager.resizeAll(), 150);
    });

    // Mobile virtual keyboard: visualViewport.height changes while window.innerHeight
    // may not, so drive body height + resize off it.
    if (window.visualViewport) {
        let vpTimeout;
        const handleViewportResize = () => {
            clearTimeout(vpTimeout);
            vpTimeout = setTimeout(() => {
                document.body.style.height = window.visualViewport.height + 'px';
                const focused = document.activeElement;
                if (focused?.classList?.contains('xterm-helper-textarea')) {
                    window.scrollTo(0, 0);
                    document.documentElement.scrollTop = 0;
                }
                TerminalManager.resizeAll();
            }, 50);
        };
        window.visualViewport.addEventListener('resize', handleViewportResize);
        window.visualViewport.addEventListener('scroll', () => {
            if (document.activeElement?.classList?.contains('xterm-helper-textarea')) {
                window.scrollTo(0, 0);
            }
        });
    }
}
