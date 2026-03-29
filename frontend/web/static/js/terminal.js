// Block <dialog> cancel event when terminal is focused
document.addEventListener('cancel', (e) => {
    if (document.activeElement?.classList?.contains('xterm-helper-textarea')) {
        e.preventDefault();
    }
}, true);

// Terminal color themes — one per app theme
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
    synthwave: {
        background: '#1a1025',
        foreground: '#e0d0ff',
        cursor: '#ff7edb',
        selectionBackground: '#553388',
        black: '#1a1025',
        red: '#fe4450',
        green: '#72f1b8',
        yellow: '#fede5d',
        blue: '#36f9f6',
        magenta: '#ff7edb',
        cyan: '#36f9f6',
        white: '#e0d0ff',
        brightBlack: '#614d85',
        brightRed: '#fe4450',
        brightGreen: '#72f1b8',
        brightYellow: '#fede5d',
        brightBlue: '#36f9f6',
        brightMagenta: '#ff7edb',
        brightCyan: '#36f9f6',
        brightWhite: '#ffffff',
    },
    cupcake: {
        background: '#2a1f2e',
        foreground: '#e8d8f0',
        cursor: '#65c3c8',
        selectionBackground: '#3d2848',
        black: '#2a1f2e',
        red: '#e07090',
        green: '#65c3c8',
        yellow: '#f0c080',
        blue: '#98b0d8',
        magenta: '#ef9fbc',
        cyan: '#80d8d8',
        white: '#e8d8f0',
        brightBlack: '#6b5074',
        brightRed: '#f090a8',
        brightGreen: '#80e8e8',
        brightYellow: '#f8d8a0',
        brightBlue: '#b0c8e8',
        brightMagenta: '#f0b8d0',
        brightCyan: '#98e8e8',
        brightWhite: '#f8f0f8',
    },
    dracula: {
        background: '#282a36',
        foreground: '#f8f8f2',
        cursor: '#f8f8f2',
        selectionBackground: '#44475a',
        black: '#21222c',
        red: '#ff5555',
        green: '#50fa7b',
        yellow: '#f1fa8c',
        blue: '#bd93f9',
        magenta: '#ff79c6',
        cyan: '#8be9fd',
        white: '#f8f8f2',
        brightBlack: '#6272a4',
        brightRed: '#ff6e6e',
        brightGreen: '#69ff94',
        brightYellow: '#ffffa5',
        brightBlue: '#d6acff',
        brightMagenta: '#ff92df',
        brightCyan: '#a4ffff',
        brightWhite: '#ffffff',
    },
    forest: {
        background: '#171212',
        foreground: '#c8d0c8',
        cursor: '#4ade80',
        selectionBackground: '#1e3328',
        black: '#171212',
        red: '#c75646',
        green: '#4ade80',
        yellow: '#c7a84a',
        blue: '#5c8a6c',
        magenta: '#8a6a8a',
        cyan: '#5ea87e',
        white: '#c8d0c8',
        brightBlack: '#3a4a3a',
        brightRed: '#e09690',
        brightGreen: '#6ee898',
        brightYellow: '#e0c06e',
        brightBlue: '#78a888',
        brightMagenta: '#b090b0',
        brightCyan: '#78c898',
        brightWhite: '#e8f0e8',
    },
    sunset: {
        background: '#1c1218',
        foreground: '#d8c8c0',
        cursor: '#e8855a',
        selectionBackground: '#3a2028',
        black: '#1c1218',
        red: '#d8605a',
        green: '#9ab87a',
        yellow: '#e8b55a',
        blue: '#7a9ac0',
        magenta: '#c08090',
        cyan: '#80b8a8',
        white: '#d8c8c0',
        brightBlack: '#5a4048',
        brightRed: '#e88078',
        brightGreen: '#b0d090',
        brightYellow: '#f0cc78',
        brightBlue: '#98b8d8',
        brightMagenta: '#d8a0b0',
        brightCyan: '#98d0c0',
        brightWhite: '#f0e8e0',
    },
    autumn: {
        background: '#1f1810',
        foreground: '#d8c8b0',
        cursor: '#d07050',
        selectionBackground: '#382818',
        black: '#1f1810',
        red: '#d07050',
        green: '#88b060',
        yellow: '#d0a048',
        blue: '#6090c0',
        magenta: '#b07898',
        cyan: '#60a080',
        white: '#d8c8b0',
        brightBlack: '#5a4830',
        brightRed: '#e89078',
        brightGreen: '#a0c878',
        brightYellow: '#e8c068',
        brightBlue: '#80b0d8',
        brightMagenta: '#c898b0',
        brightCyan: '#78c0a0',
        brightWhite: '#f0e8d8',
    },
    coffee: {
        background: '#1a1412',
        foreground: '#c8b8a8',
        cursor: '#c8955a',
        selectionBackground: '#382820',
        black: '#1a1412',
        red: '#c06050',
        green: '#88a070',
        yellow: '#c8955a',
        blue: '#708898',
        magenta: '#a07880',
        cyan: '#709088',
        white: '#c8b8a8',
        brightBlack: '#4a3830',
        brightRed: '#d88070',
        brightGreen: '#a0b888',
        brightYellow: '#e0b078',
        brightBlue: '#90a8b8',
        brightMagenta: '#c098a0',
        brightCyan: '#90b0a8',
        brightWhite: '#e8ddd0',
    },
    business: {
        background: '#1c1e26',
        foreground: '#cbced4',
        cursor: '#36a3d9',
        selectionBackground: '#2a3040',
        black: '#1c1e26',
        red: '#e06c75',
        green: '#98c379',
        yellow: '#e5c07b',
        blue: '#36a3d9',
        magenta: '#c678dd',
        cyan: '#56b6c2',
        white: '#cbced4',
        brightBlack: '#4a4e5a',
        brightRed: '#f09898',
        brightGreen: '#b0d898',
        brightYellow: '#f0d898',
        brightBlue: '#68c0e8',
        brightMagenta: '#d8a0e8',
        brightCyan: '#78d0d8',
        brightWhite: '#e8eaf0',
    },
};

function getTerminalTheme() {
    const appTheme = document.documentElement.getAttribute('data-theme') || 'dark';
    return terminalThemes[appTheme] || terminalThemes.dark;
}

// CSS variable for terminal background — keeps style.css in sync with xterm
function syncTerminalBgVar() {
    const theme = getTerminalTheme();
    document.documentElement.style.setProperty('--terminal-bg', theme.background);
    // Determine if this is a light-base or dark-base theme for CSS overrides
    const lightThemes = ['light', 'cupcake', 'autumn'];
    const isLight = lightThemes.includes(document.documentElement.getAttribute('data-theme'));
    document.documentElement.setAttribute('data-theme-base', isLight ? 'light' : 'dark');
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

        // GPU-accelerated rendering (desktop only — saves battery on mobile).
        let webglAddon = null;
        if (!isMobile && typeof WebglAddon !== 'undefined') {
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
            // Block xterm from sending raw \x16 for Ctrl+V — we handle paste
            // via a capture-phase paste listener on the textarea instead.
            if (e.key === 'v' && (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) {
                return false;
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

            // Capture-phase paste listener: runs before xterm's handler (which
            // calls stopPropagation). If the clipboard contains an image, we
            // block xterm, upload the image, and send the path as terminal input.
            // Text-only pastes fall through to xterm's normal handling.
            textarea.addEventListener('paste', (e) => {
                const items = e.clipboardData?.items;
                if (!items) return;

                for (const item of items) {
                    if (!item.type.startsWith('image/')) continue;

                    // Image found — block xterm from handling this paste.
                    e.stopImmediatePropagation();
                    e.preventDefault();

                    const blob = item.getAsFile();
                    if (!blob) return;

                    const formData = new FormData();
                    formData.append('image', blob, 'clipboard.' + blob.type.split('/')[1]);

                    fetch(`/api/sessions/${terminalId}/upload`, {
                        method: 'POST',
                        body: formData,
                    }).then(async (resp) => {
                        if (!resp.ok) {
                            const err = await resp.json().catch(() => ({}));
                            term.write(`\r\n\x1b[31m[Upload failed: ${err.error || resp.statusText}]\x1b[0m`);
                            return;
                        }
                        const { path } = await resp.json();
                        const ws = this.instances[terminalId]?.ws;
                        if (ws && ws.readyState === WebSocket.OPEN) {
                            ws.send(new TextEncoder().encode(path));
                        }
                    }).catch((err) => {
                        term.write(`\r\n\x1b[31m[Upload failed: ${err.message}]\x1b[0m`);
                    });
                    return;
                }
            }, { capture: true });
        }

        // Mobile: xterm.js v6 replaced native viewport scroll with a programmatic
        // ScrollableElement. Touch swipes no longer scroll. We drive the
        // ScrollableElement's pixel-level setScrollPosition directly for
        // smooth sub-line scrolling with momentum.
        if (isMobile) {
            const viewport = containerEl.querySelector('.xterm-viewport');
            if (viewport) {
                viewport.addEventListener('click', () => term.focus());
            }
            const getSE = () => term._core?._viewport?._scrollableElement;
            let lastTouchY = 0;
            let momentumRaf = 0;
            const samples = [];
            const maxSamples = 5;

            containerEl.addEventListener('touchstart', (e) => {
                if (e.touches.length === 1) {
                    cancelAnimationFrame(momentumRaf);
                    lastTouchY = e.touches[0].clientY;
                    samples.length = 0;
                }
            }, { passive: true });

            containerEl.addEventListener('touchmove', (e) => {
                if (e.touches.length !== 1) return;
                const currentY = e.touches[0].clientY;
                const dy = lastTouchY - currentY;
                lastTouchY = currentY;
                samples.push({ dy, t: performance.now() });
                if (samples.length > maxSamples) samples.shift();
                const se = getSE();
                if (!se) return;
                const pos = se.getScrollPosition();
                se.setScrollPosition({ scrollTop: pos.scrollTop + dy });
            }, { passive: true });

            containerEl.addEventListener('touchend', () => {
                if (samples.length < 2) return;
                const se = getSE();
                if (!se) return;
                const first = samples[0];
                const last = samples[samples.length - 1];
                const dt = last.t - first.t;
                if (dt <= 0) return;
                const totalDy = samples.reduce((sum, s) => sum + s.dy, 0);
                let vel = (totalDy / dt) * 16;

                const coast = () => {
                    vel *= 0.96;
                    if (Math.abs(vel) < 0.5) return;
                    const pos = se.getScrollPosition();
                    se.setScrollPosition({ scrollTop: pos.scrollTop + vel });
                    momentumRaf = requestAnimationFrame(coast);
                };
                momentumRaf = requestAnimationFrame(coast);
            }, { passive: true });
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
            webglAddon,
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
