// SessionSocket: owns one session WebSocket's full lifecycle as an explicit
// state machine. Everything that talks to the session goes through send() —
// no caller touches the raw WebSocket.
//
// States: idle → connecting → open → (reconnecting ⇄ open) → ended | lost | closed
//   ended  — server closed normally (session exited); terminal state.
//   lost   — reconnect attempts exhausted; retry() re-arms.
//   closed — close() was called (tab closed); terminal state.

const MAX_RECONNECT_ATTEMPTS = 10;
// Start fast: a relay is briefly absent after a backend restart/rebuild, so the
// first retries recover sub-second. Doubles to the cap from there.
const RECONNECT_BASE_DELAY = 250;    // ms — doubles each retry
const RECONNECT_MAX_DELAY = 30000;   // ms — 30 second cap
const WS_NORMAL_CLOSURE = 1000;

export class SessionSocket {
    // callbacks: onData(bytes|string), onControl(msg), onStatus(status, info).
    constructor(terminalId, callbacks = {}) {
        this.terminalId = terminalId;
        this.callbacks = callbacks;
        this.status = 'idle';
        this.ws = null;
        this.retryCount = 0;
        this.retryTimer = null;
    }

    _setStatus(status, info = {}) {
        this.status = status;
        this.callbacks.onStatus?.(status, info);
    }

    connect() {
        const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(`${protocol}//${location.host}/ws/terminal/${this.terminalId}`);
        ws.binaryType = 'arraybuffer';
        this.ws = ws;
        this.status = this.retryCount > 0 ? 'reconnecting' : 'connecting';

        ws.onopen = () => {
            const resumed = this.retryCount > 0;
            this.retryCount = 0;
            this._setStatus('open', { resumed });
        };

        ws.onmessage = (event) => {
            if (event.data instanceof ArrayBuffer) {
                this.callbacks.onData?.(new Uint8Array(event.data));
                return;
            }
            // Text messages are JSON control messages from the server.
            try {
                this.callbacks.onControl?.(JSON.parse(event.data));
            } catch (e) {
                this.callbacks.onData?.(event.data);
            }
        };

        ws.onerror = () => {};
        ws.onclose = (event) => this._onClose(event);
    }

    _onClose(event) {
        // Tab closed / terminal destroyed — a client-initiated close reports code
        // 1005 (no status), not WS_NORMAL_CLOSURE, so key off our own flag or the
        // reconnect would spawn a zombie viewer that suspends the real one.
        if (this.status === 'closed') return;

        if (event.code === WS_NORMAL_CLOSURE) {
            this._setStatus('ended');
            return;
        }
        if (this.retryCount >= MAX_RECONNECT_ATTEMPTS) {
            this._setStatus('lost');
            return;
        }
        this.retryCount++;
        const delay = Math.min(RECONNECT_BASE_DELAY * Math.pow(2, this.retryCount - 1), RECONNECT_MAX_DELAY);
        this._setStatus('reconnecting', { attempt: this.retryCount, delay });
        this.retryTimer = setTimeout(() => {
            this.retryTimer = null;
            this.connect();
        }, delay);
    }

    // send delivers bytes to the session; false when the socket isn't open.
    send(bytes) {
        if (this.status !== 'open' || this.ws?.readyState !== WebSocket.OPEN) return false;
        this.ws.send(bytes);
        return true;
    }

    // sendResize reports the terminal dimensions (JSON control message).
    sendResize(cols, rows) {
        if (this.status !== 'open' || this.ws?.readyState !== WebSocket.OPEN) return false;
        this.ws.send(JSON.stringify({ type: 'resize', cols, rows }));
        return true;
    }

    // retry re-arms a lost connection (manual retry after exhausted backoff).
    retry() {
        if (this.status !== 'lost') return;
        this.retryCount = 0;
        this.connect();
    }

    // close ends the socket intentionally: no reconnect, no further callbacks.
    close() {
        this.status = 'closed';
        if (this.retryTimer != null) {
            clearTimeout(this.retryTimer);
            this.retryTimer = null;
        }
        if (this.ws && this.ws.readyState !== WebSocket.CLOSED) {
            this.ws.close();
        }
    }
}
