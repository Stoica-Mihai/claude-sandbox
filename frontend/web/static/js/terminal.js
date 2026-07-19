// Terminal manager: owns the xterm.js instances and coordinates their concerns
// (theme, socket relay, clipboard, mobile touch), each of which lives in a
// sibling terminal-*.js module.

import { isMobile } from './ui-utils.js';
import { getTerminalTheme, syncTerminalBgVar, terminalThemes } from './terminal-theme.js';
import { SessionSocket } from './session-socket.js';
import { ANSI_RED, ANSI_GRAY, ANSI_RESET } from './terminal-ansi.js';
import { wireClipboard } from './terminal-clipboard.js';
import { wireTouchScroll } from './terminal-touch.js';

// Re-export the theme surface so existing importers (theme.js, tests) keep
// importing it from terminal.js.
export { terminalThemes, getTerminalTheme, syncTerminalBgVar };

// TerminalManager — manages multiple xterm.js instances and their sockets.
export const TerminalManager = {
    instances: {}, // terminalId -> { term, socket, fitAddon, webLinksAddon, webglAddon, containerId, needsRefresh }

    create(terminalId, containerEl) {
        if (this.instances[terminalId]) {
            this.destroy(terminalId);
        }

        const fitAddon = new FitAddon.FitAddon();
        const webLinksAddon = new WebLinksAddon.WebLinksAddon();

        const mobile = isMobile();
        const term = new Terminal({
            cursorBlink: true,
            rightClickSelectsWord: !mobile,
            // Smaller on mobile so more columns fit per line on a narrow screen.
            fontSize: mobile ? 12 : 14,
            fontFamily: "Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
            lineHeight: 1.15,
            theme: getTerminalTheme(),
            allowProposedApi: true,
        });

        term.loadAddon(fitAddon);
        term.loadAddon(webLinksAddon);

        // GPU-accelerated rendering (desktop only — saves battery on mobile).
        let webglAddon = null;
        if (!mobile && typeof WebglAddon !== 'undefined') {
            try {
                webglAddon = new WebglAddon.WebglAddon();
                webglAddon.onContextLoss(() => {
                    webglAddon.dispose();
                    webglAddon = null;
                });
                term.loadAddon(webglAddon);
            } catch (e) {
                webglAddon = null;
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

        wireClipboard(containerEl, term, terminalId, this, mobile);
        if (mobile) {
            wireTouchScroll(containerEl, term);
        }

        // Fit after opening (needs a frame for container dimensions).
        requestAnimationFrame(() => {
            try {
                fitAddon.fit();
            } catch (e) { /* container may not be visible yet */ }
        });

        const instance = {
            term,
            socket: null,
            fitAddon,
            webLinksAddon,
            webglAddon,
            containerId: containerEl.id,
            needsRefresh: false, // set when another viewer resized us; our display is garbled until next input
        };
        this.instances[terminalId] = instance;

        let scrollRafPending = false;
        const socket = new SessionSocket(terminalId, {
            onData: (data) => {
                const buf = term.buffer.active;
                const atBottom = buf.baseY <= buf.viewportY + 5;
                term.write(data);
                if (atBottom && !scrollRafPending) {
                    scrollRafPending = true;
                    requestAnimationFrame(() => {
                        // Skip if the terminal was disposed/replaced before this frame ran.
                        if (this.instances[terminalId]?.term === term) term.scrollToBottom();
                        scrollRafPending = false;
                    });
                }
            },
            onControl: (msg) => {
                if (msg.type === 'deactivated') instance.needsRefresh = true;
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
        instance.socket = socket;
        socket.connect();

        // Focusing a suspended terminal takes the live view back WITHOUT typing:
        // ask the server to reactivate us and repaint (a fresh snapshot arrives).
        // This is the passive counterpart to the input-driven takeover below —
        // it injects nothing into the session.
        containerEl.addEventListener('focusin', () => {
            if (socket.status === 'open' && instance.needsRefresh) {
                instance.needsRefresh = false;
                socket.sendControl({ type: 'reactivate' });
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
            if (socket.status === 'open' && instance.needsRefresh) {
                instance.needsRefresh = false;
                term.clear();
            }
            socket.send(new TextEncoder().encode(data));
            // On mobile, dismiss the keyboard after Enter so output is visible.
            if (data === '\r' && isMobile()) {
                const ta = containerEl.querySelector('.xterm-helper-textarea');
                if (ta) setTimeout(() => ta.blur(), 50);
            }
        });

        term.onResize(({ cols, rows }) => {
            socket.sendResize(cols, rows);
        });

        return term;
    },

    destroy(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;

        instance.socket?.close(); // intentional: no reconnect
        if (instance.webglAddon) {
            instance.webglAddon.dispose();
        }
        instance.term.dispose();
        delete this.instances[terminalId];
    },

    resize(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        try {
            instance.fitAddon.fit();
            instance.term.scrollToBottom();
        } catch (e) { /* container may not be visible */ }
    },

    resizeAll() {
        for (const terminalId of Object.keys(this.instances)) {
            this.resize(terminalId);
        }
    },

    get(terminalId) {
        return this.instances[terminalId] || null;
    },

    rethemeAll() {
        const theme = getTerminalTheme();
        syncTerminalBgVar();
        for (const instance of Object.values(this.instances)) {
            instance.term.options.theme = theme;
        }
    },

    getByContainer(containerId) {
        for (const [id, instance] of Object.entries(this.instances)) {
            if (instance.containerId === containerId) {
                return { terminalId: id, ...instance };
            }
        }
        return null;
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
