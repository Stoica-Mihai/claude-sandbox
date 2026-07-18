// Session WebSocket relay: connect, stream output to the xterm, and reconnect
// with backoff. Per-connection state lives on `instance` (ws, retryCount,
// retryTimer, closing, needsRefresh) so the reconnect path and the input handler
// in terminal.js share it.

import { ANSI_RED, ANSI_GRAY, ANSI_RESET } from './terminal-ansi.js';

const MAX_RECONNECT_ATTEMPTS = 10;
// Start fast: a relay is briefly absent after a backend restart/rebuild (in-memory
// relays rebuild on the next discovery), so the first retries recover sub-second
// instead of stalling on a full-second wait. Doubles to the cap from there.
const RECONNECT_BASE_DELAY = 250;    // ms — doubles each retry
const RECONNECT_MAX_DELAY = 30000;   // ms — 30 second cap
const WS_NORMAL_CLOSURE = 1000;

// Open (or reopen) the session socket and wire its handlers. `manager` is used to
// skip stale scroll corrections after the terminal is disposed/replaced.
export function connectWs(manager, instance, term, terminalId) {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${location.host}/ws/terminal/${terminalId}`;
    const ws = new WebSocket(wsUrl);
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => {
        if (instance.retryCount > 0) {
            instance.retryCount = 0;
            // The server replays its full ring buffer on every (re)attach. Our
            // buffer still holds the pre-disconnect scrollback, so without a reset
            // the replay appends a second copy (the duplication seen on mobile,
            // where the socket drops often). Reset to resync to the server buffer.
            term.reset();
        }
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
    };

    let scrollRafPending = false;
    ws.onmessage = (event) => {
        if (event.data instanceof ArrayBuffer) {
            const buf = term.buffer.active;
            const atBottom = buf.baseY <= buf.viewportY + 5;
            term.write(new Uint8Array(event.data));
            if (atBottom && !scrollRafPending) {
                scrollRafPending = true;
                requestAnimationFrame(() => {
                    // Skip if the terminal was disposed/replaced before this frame ran.
                    if (manager.instances[terminalId]?.term === term) term.scrollToBottom();
                    scrollRafPending = false;
                });
            }
        } else {
            // Text messages are JSON control messages from the server.
            try {
                const msg = JSON.parse(event.data);
                if (msg.type === 'deactivated') {
                    instance.needsRefresh = true;
                }
            } catch (e) {
                term.write(event.data);
            }
        }
    };

    ws.onerror = () => {};

    ws.onclose = (event) => {
        // Tab closed / terminal destroyed — a client-initiated close reports code
        // 1005 (no status), which is not WS_NORMAL_CLOSURE, so without this guard
        // the reconnect below would spawn a zombie viewer that suspends the real
        // one on reopen (blank terminal).
        if (instance.closing) {
            return;
        }
        // Normal closure — session ended, no reconnect.
        if (event.code === WS_NORMAL_CLOSURE) {
            term.write(`\r\n${ANSI_GRAY}[Session ended]${ANSI_RESET}\r\n`);
            return;
        }
        // Unexpected closure — attempt reconnection.
        if (instance.retryCount >= MAX_RECONNECT_ATTEMPTS) {
            term.write(`\r\n${ANSI_RED}[Connection lost]${ANSI_RESET}\r\n`);
            return;
        }
        instance.retryCount++;
        const delay = Math.min(RECONNECT_BASE_DELAY * Math.pow(2, instance.retryCount - 1), RECONNECT_MAX_DELAY);
        term.write(`\r\n${ANSI_GRAY}[Reconnecting... (attempt ${instance.retryCount})]${ANSI_RESET}`);
        instance.retryTimer = setTimeout(() => {
            instance.retryTimer = null;
            connectWs(manager, instance, term, terminalId);
        }, delay);
    };

    instance.ws = ws;
}
