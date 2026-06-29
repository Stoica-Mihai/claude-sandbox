// Block <dialog> cancel event when terminal is focused
document.addEventListener('cancel', (e) => {
    if (document.activeElement?.classList?.contains('xterm-helper-textarea')) {
        e.preventDefault();
    }
}, true);

// Clipboard fallback for contexts where the async Clipboard API is unavailable.
function copyFallback(text) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); } catch (e) { /* best effort */ }
    document.body.removeChild(ta);
}

// ANSI escape codes for terminal status messages
const ANSI_RED = '\x1b[31m';
const ANSI_GREEN = '\x1b[32m';
const ANSI_GRAY = '\x1b[90m';
const ANSI_RESET = '\x1b[0m';

// WebSocket reconnection
const MAX_RECONNECT_ATTEMPTS = 10;
const RECONNECT_BASE_DELAY = 1000;   // ms — doubles each retry
const RECONNECT_MAX_DELAY = 30000;   // ms — 30 second cap
const WS_NORMAL_CLOSURE = 1000;

// Touch momentum physics
const MOMENTUM_FRICTION = 0.96;
const MOMENTUM_MIN_VELOCITY = 0.5;
const MOMENTUM_MS_PER_FRAME = 16;
const MAX_TOUCH_SAMPLES = 5;


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
};

function getTerminalTheme() {
    const appTheme = document.documentElement.getAttribute('data-theme') || 'dark';
    return terminalThemes[appTheme] || terminalThemes.dark;
}

// CSS variable for terminal background — keeps style.css in sync with xterm
function syncTerminalBgVar() {
    const theme = getTerminalTheme();
    document.documentElement.style.setProperty('--terminal-bg', theme.background);
    document.documentElement.style.setProperty('--terminal-fg', theme.foreground);
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

        const mobile = isMobile();
        const term = new Terminal({
            cursorBlink: true,
            rightClickSelectsWord: !mobile,
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

        // Copy-on-select (desktop): xterm has no built-in copyOnSelect, so copy
        // the final selection on mouseup. Uses the async Clipboard API with an
        // execCommand fallback for non-secure contexts.
        if (!mobile) {
            containerEl.addEventListener('mouseup', () => {
                const sel = term.getSelection();
                if (!sel) return;
                if (navigator.clipboard?.writeText) {
                    navigator.clipboard.writeText(sel).catch(() => copyFallback(sel));
                } else {
                    copyFallback(sel);
                }
            });
        }

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
                            term.write(`\r\n${ANSI_RED}[Upload failed: ${err.error || resp.statusText}]${ANSI_RESET}`);
                            return;
                        }
                        const { path } = await resp.json();
                        const ws = this.instances[terminalId]?.ws;
                        if (ws && ws.readyState === WebSocket.OPEN) {
                            ws.send(new TextEncoder().encode(path));
                        }
                    }).catch((err) => {
                        term.write(`\r\n${ANSI_RED}[Upload failed: ${err.message}]${ANSI_RESET}`);
                    });
                    return;
                }
            }, { capture: true });
        }

        // Mobile: xterm.js v6 replaced native viewport scroll with a programmatic
        // ScrollableElement. Touch swipes no longer scroll. We drive the
        // ScrollableElement's pixel-level setScrollPosition directly for
        // smooth sub-line scrolling with momentum.
        if (mobile) {
            const viewport = containerEl.querySelector('.xterm-viewport');
            if (viewport) {
                viewport.addEventListener('click', () => term.focus());
            }
            const getSE = () => term._core?._viewport?._scrollableElement;
            let lastTouchY = 0;
            let momentumRaf = 0;
            const samples = [];
            const maxSamples = MAX_TOUCH_SAMPLES;

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
                let vel = (totalDy / dt) * MOMENTUM_MS_PER_FRAME;

                const coast = () => {
                    vel *= MOMENTUM_FRICTION;
                    if (Math.abs(vel) < MOMENTUM_MIN_VELOCITY) return;
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

        // Track whether another viewer resized the session (our display is garbled).
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
                    term.write(`\r\n${ANSI_GREEN}[Reconnected]${ANSI_RESET}`);
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
                            // Skip if the terminal was disposed (tab closed / reload) before this frame ran
                            if (TerminalManager.instances[terminalId]?.term === term) term.scrollToBottom();
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
                if (event.code === WS_NORMAL_CLOSURE) {
                    term.write(`\r\n${ANSI_GRAY}[Session ended]${ANSI_RESET}\r\n`);
                    return;
                }

                // Unexpected closure — attempt reconnection
                if (retryCount >= MAX_RECONNECT_ATTEMPTS) {
                    term.write(`\r\n${ANSI_RED}[Connection lost]${ANSI_RESET}\r\n`);
                    return;
                }

                retryCount++;
                const delay = Math.min(RECONNECT_BASE_DELAY * Math.pow(2, retryCount - 1), RECONNECT_MAX_DELAY);
                term.write(`\r\n${ANSI_GRAY}[Reconnecting... (attempt ${retryCount})]${ANSI_RESET}`);
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
                // If another viewer resized the session, clear our garbled
                // display. The input triggers ResizeToViewer on the server,
                // which resizes the PTY back to our dimensions; the redraw
                // arrives through normal broadcast.
                if (needsRefresh) {
                    needsRefresh = false;
                    term.clear();
                }
                const encoder = new TextEncoder();
                ws.send(encoder.encode(data));
            }
            // On mobile, dismiss keyboard after Enter so the user can see output
            if (data === '\r' && isMobile()) {
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
        badgeText.textContent = `${count} session${count !== 1 ? 's' : ''}`;
        badgeText.classList.toggle('alive', count > 0);
    }
});

document.addEventListener('DOMContentLoaded', () => {
    const sessionList = document.getElementById('session-list');
    if (sessionList) {
        observer.observe(sessionList, { childList: true, subtree: true });
    }
});
