// Block <dialog> cancel event when terminal is focused
document.addEventListener('cancel', (e) => {
    if (document.activeElement?.classList?.contains('xterm-helper-textarea')) {
        e.preventDefault();
    }
}, true);

// Terminal color themes
const terminalThemes = {
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
        selectionBackground: '#b6d4fe',
        black: '#24292f',
        red: '#cf222e',
        green: '#116329',
        yellow: '#4d2d00',
        blue: '#0969da',
        magenta: '#8250df',
        cyan: '#1b7c83',
        white: '#6e7781',
        brightBlack: '#57606a',
        brightRed: '#a40e26',
        brightGreen: '#1a7f37',
        brightYellow: '#633c01',
        brightBlue: '#218bff',
        brightMagenta: '#a475f9',
        brightCyan: '#3192aa',
        brightWhite: '#8c959f',
    }
};

function getTerminalTheme() {
    const appTheme = document.documentElement.getAttribute('data-theme') || 'dark';
    return terminalThemes[appTheme] || terminalThemes.dark;
}

// TerminalManager — manages multiple xterm.js instances and WebSocket connections
const TerminalManager = {
    instances: {}, // terminalId -> {term, ws, fitAddon, webLinksAddon, containerId, retryTimer}

    create(terminalId, containerEl) {
        // Destroy existing instance for this terminal if present
        if (this.instances[terminalId]) {
            this.destroy(terminalId);
        }

        const fitAddon = new FitAddon.FitAddon();
        const webLinksAddon = new WebLinksAddon.WebLinksAddon();

        const isMobile = window.matchMedia('(max-width: 767px)').matches;
        const term = new Terminal({
            cursorBlink: true,
            copyOnSelect: !isMobile,
            rightClickSelectsWord: !isMobile,
            fontSize: 14,
            fontFamily: "Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
            lineHeight: 1.15,
            theme: getTerminalTheme(),
            allowProposedApi: true,
        });

        term.loadAddon(fitAddon);
        term.loadAddon(webLinksAddon);

        // Prevent the browser from intercepting keys that Claude Code needs
        term.attachCustomKeyEventHandler((e) => {
            if (e.type !== 'keydown') return true;
            // Let Alt+key combos pass through to our shortcut handler (don't send to PTY)
            if (e.altKey && !e.ctrlKey && !e.metaKey) {
                return false;
            }
            // Block browser default for Escape, Ctrl+C, Ctrl+D, etc.
            if (e.key === 'Escape' || (e.ctrlKey && ['c','d','z','l'].includes(e.key))) {
                e.preventDefault();
                e.stopPropagation();
            }
            return true;
        });

        term.open(containerEl);

        // Prevent Escape from unfocusing the terminal.
        // The browser blurs the textarea on Escape at a level preventDefault can't stop.
        // We re-focus it immediately via the textarea's own blur listener.
        const textarea = containerEl.querySelector('.xterm-helper-textarea');
        if (textarea) {
            let lastKeyEscape = false;
            textarea.addEventListener('keydown', (e) => {
                lastKeyEscape = (e.key === 'Escape');
            });
            textarea.addEventListener('blur', () => {
                if (lastKeyEscape) {
                    lastKeyEscape = false;
                    setTimeout(() => textarea.focus(), 0);
                }
            });
        }

        // Mobile: add click-to-focus since the viewport overlay is now on top
        // of the screen (via CSS z-index) for native scroll support.
        if (isMobile) {
            const viewport = containerEl.querySelector('.xterm-viewport');
            if (viewport) {
                viewport.addEventListener('click', () => term.focus());
            }
        }

        // Fit after opening (needs a frame for container dimensions)
        requestAnimationFrame(() => {
            try {
                fitAddon.fit();
            } catch(e) {}
        });

        // Track whether another viewer resized tmux (our display is garbled).
        let needsRefresh = false;

        // Reconnection state
        let retryCount = 0;

        // Store instance early so connectWs() can mutate it
        const instance = {
            term,
            ws: null,
            fitAddon,
            webLinksAddon,
            containerId: containerEl.id,
            retryTimer: null
        };
        this.instances[terminalId] = instance;

        // Connect (or reconnect) the WebSocket
        const connectWs = () => {
            const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = `${protocol}//${location.host}/ws/terminal/${terminalId}`;
            const ws = new WebSocket(wsUrl);
            ws.binaryType = 'arraybuffer';

            ws.onopen = () => {
                if (retryCount > 0) {
                    retryCount = 0;
                    term.write('\r\n\x1b[32m[Reconnected]\x1b[0m');
                }
                // Send initial resize
                const resizeMsg = JSON.stringify({
                    type: 'resize',
                    cols: term.cols,
                    rows: term.rows
                });
                ws.send(resizeMsg);
            };

            let scrollRafPending = false;
            ws.onmessage = (event) => {
                if (event.data instanceof ArrayBuffer) {
                    const buf = term.buffer.active;
                    const atBottom = buf.baseY <= buf.viewportY + 5;
                    term.write(new Uint8Array(event.data));
                    // Coalesce scroll corrections into a single rAF
                    if (atBottom && !scrollRafPending) {
                        scrollRafPending = true;
                        requestAnimationFrame(() => {
                            term.scrollToBottom();
                            scrollRafPending = false;
                        });
                    }
                } else {
                    // Text messages are JSON control messages from the server.
                    try {
                        const msg = JSON.parse(event.data);
                        if (msg.type === 'deactivated') {
                            needsRefresh = true;
                        }
                    } catch (e) {
                        term.write(event.data);
                    }
                }
            };

            ws.onerror = () => {};

            ws.onclose = (event) => {
                // Normal closure — session ended, no reconnect
                if (event.code === 1000) {
                    term.write('\r\n\x1b[90m[Session ended]\x1b[0m\r\n');
                    return;
                }

                // Unexpected closure — attempt reconnection
                if (retryCount >= 10) {
                    term.write('\r\n\x1b[31m[Connection lost]\x1b[0m\r\n');
                    return;
                }

                retryCount++;
                const delay = Math.min(1000 * Math.pow(2, retryCount - 1), 30000);
                term.write(`\r\n\x1b[90m[Reconnecting... (attempt ${retryCount})]\x1b[0m`);
                instance.retryTimer = setTimeout(() => {
                    instance.retryTimer = null;
                    connectWs();
                }, delay);
            };

            instance.ws = ws;
        };

        connectWs();

        // User input -> WebSocket (send as binary so Go routes it to PTY)
        term.onData((data) => {
            const ws = instance.ws;
            if (ws && ws.readyState === WebSocket.OPEN) {
                // If another viewer resized tmux, clear our garbled display.
                // The input triggers ResizeToViewer on the server, which
                // resizes tmux back to our dimensions. tmux redraws and we
                // get clean content through normal broadcast.
                if (needsRefresh) {
                    needsRefresh = false;
                    term.clear();
                }
                const encoder = new TextEncoder();
                ws.send(encoder.encode(data));
            }
            // On mobile, dismiss keyboard after Enter so the user can see output
            if (data === '\r' && window.matchMedia('(max-width: 767px)').matches) {
                const ta = containerEl.querySelector('.xterm-helper-textarea');
                if (ta) setTimeout(() => ta.blur(), 50);
            }
        });

        // Handle terminal resize
        term.onResize(({ cols, rows }) => {
            const ws = instance.ws;
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({
                    type: 'resize',
                    cols: cols,
                    rows: rows
                }));
            }
        });

        return term;
    },

    destroy(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;

        if (instance.retryTimer != null) {
            clearTimeout(instance.retryTimer);
            instance.retryTimer = null;
        }
        if (instance.ws && instance.ws.readyState !== WebSocket.CLOSED) {
            instance.ws.close();
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
        } catch (e) {
            // Container may not be visible
        }
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
    }
};

// Debounced window resize handler
let resizeTimeout;
window.addEventListener('resize', () => {
    clearTimeout(resizeTimeout);
    resizeTimeout = setTimeout(() => {
        TerminalManager.resizeAll();
    }, 150);
});

// Mobile virtual keyboard handling — when the keyboard opens/closes,
// visualViewport.height changes but window.innerHeight may not.
if (window.visualViewport) {
    let vpTimeout;
    const handleViewportResize = () => {
        clearTimeout(vpTimeout);
        vpTimeout = setTimeout(() => {
            const vh = window.visualViewport.height;
            // Set body and main content height to visual viewport
            document.body.style.height = vh + 'px';
            // Scroll the page so the focused element is visible
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
        // Prevent the browser from scrolling the viewport when keyboard opens
        if (document.activeElement?.classList?.contains('xterm-helper-textarea')) {
            window.scrollTo(0, 0);
        }
    });
}

// Update session badge count when session list updates
const observer = new MutationObserver(() => {
    const countEl = document.getElementById('session-count');
    const badgeText = document.getElementById('session-badge-text');
    if (countEl && badgeText) {
        const count = parseInt(countEl.textContent, 10) || 0;
        badgeText.innerHTML = '<span class="hidden md:inline">' + count + ' session' + (count !== 1 ? 's' : '') + '</span><span class="md:hidden">' + count + '</span>';
        // Pulse green when sessions are active
        if (count > 0) {
            badgeText.classList.add('text-emerald-500', 'pulse-alive');
        } else {
            badgeText.classList.remove('text-emerald-500', 'pulse-alive');
        }
    }
});

document.addEventListener('DOMContentLoaded', () => {
    const sessionList = document.getElementById('session-list');
    if (sessionList) {
        observer.observe(sessionList, { childList: true, subtree: true });
    }
});
