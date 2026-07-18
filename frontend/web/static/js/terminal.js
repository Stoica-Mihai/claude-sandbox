// Terminal manager: owns the xterm.js instances and coordinates their concerns
// (theme, socket relay, clipboard, mobile touch), each of which lives in a
// sibling terminal-*.js module.

import { isMobile } from './ui-utils.js';
import { getTerminalTheme, syncTerminalBgVar, terminalThemes } from './terminal-theme.js';
import { connectWs } from './terminal-connection.js';
import { wireClipboard } from './terminal-clipboard.js';
import { wireTouchScroll } from './terminal-touch.js';

// Re-export the theme surface so existing importers (theme.js, tests) keep
// importing it from terminal.js.
export { terminalThemes, getTerminalTheme, syncTerminalBgVar };

// TerminalManager — manages multiple xterm.js instances and their sockets.
export const TerminalManager = {
    instances: {}, // terminalId -> { term, ws, fitAddon, webLinksAddon, webglAddon, containerId, retryTimer, retryCount, needsRefresh, closing }

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
            ws: null,
            fitAddon,
            webLinksAddon,
            webglAddon,
            containerId: containerEl.id,
            retryTimer: null,
            retryCount: 0,
            needsRefresh: false, // set when another viewer resized us; our display is garbled until next input
            closing: false,      // set by destroy() so onclose doesn't reconnect an intentionally-closed tab
        };
        this.instances[terminalId] = instance;

        connectWs(this, instance, term, terminalId);

        // User input -> WebSocket (binary so Go routes it to the PTY).
        term.onData((data) => {
            const ws = instance.ws;
            if (ws && ws.readyState === WebSocket.OPEN) {
                // If another viewer resized the session, clear our garbled display.
                // The input triggers ResizeToViewer on the server, which resizes the
                // PTY back to our dimensions; the redraw arrives via normal broadcast.
                if (instance.needsRefresh) {
                    instance.needsRefresh = false;
                    term.clear();
                }
                ws.send(new TextEncoder().encode(data));
            }
            // On mobile, dismiss the keyboard after Enter so output is visible.
            if (data === '\r' && isMobile()) {
                const ta = containerEl.querySelector('.xterm-helper-textarea');
                if (ta) setTimeout(() => ta.blur(), 50);
            }
        });

        term.onResize(({ cols, rows }) => {
            const ws = instance.ws;
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: 'resize', cols, rows }));
            }
        });

        return term;
    },

    destroy(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;

        // Mark the close intentional before closing so the socket's onclose does
        // not treat it as an unexpected drop and reconnect.
        instance.closing = true;
        if (instance.retryTimer != null) {
            clearTimeout(instance.retryTimer);
            instance.retryTimer = null;
        }
        if (instance.ws && instance.ws.readyState !== WebSocket.CLOSED) {
            instance.ws.close();
        }
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

    // Keep the header session-badge count in sync with the session list.
    const observer = new MutationObserver(() => {
        const countEl = document.getElementById('session-count');
        const badgeText = document.getElementById('session-badge-text');
        if (countEl && badgeText) {
            const count = parseInt(countEl.textContent, 10) || 0;
            badgeText.textContent = `${count} session${count !== 1 ? 's' : ''}`;
            badgeText.classList.toggle('alive', count > 0);
        }
    });
    const sessionList = document.getElementById('session-list');
    if (sessionList) {
        observer.observe(sessionList, { childList: true, subtree: true });
    }
}
