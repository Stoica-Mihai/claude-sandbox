// Chat surface manager: owns one chat session's DOM (header, message list,
// input bar) and its SessionSocket, mirroring TerminalManager's shape
// (create/destroy/get) so tabs.js can treat either surface uniformly.

import { SessionSocket } from './session-socket.js';
import { createChatState, applyEvent, composeUserInput } from './chat-events.js';
import { applyPatches, appendUserMessage, appendSystemNotice, resetView, showPending, clearPending } from './chat-render.js';
import { createStickyScroll } from './chat-scroll.js';
import { createInputBar } from './chat-input.js';
import { sessionTranscriptPath, sessionPath, sessionModePath } from './routes.js';
import { getSession } from './store.js';

// TAIL_INITIAL_LINES/TAIL_CHUNK bound how much transcript history renders
// eagerly (§7 of the chat-sessions design: tail-first, lazy-load older turns).
const TAIL_INITIAL_LINES = 200;
const TAIL_CHUNK = 200;

export const ChatManager = {
    instances: {}, // terminalId -> instance (see create())

    create(terminalId, containerEl) {
        if (this.instances[terminalId]) this.destroy(terminalId);

        containerEl.innerHTML = '';
        const root = document.createElement('div');
        root.className = 'chat-surface';

        const header = document.createElement('div');
        header.className = 'chat-header';
        const headerCwd = document.createElement('span');
        headerCwd.className = 'chat-header-cwd';
        // The live system/init event fires only at process start, so a viewer
        // joining later would see an empty header — seed cwd from the store.
        headerCwd.textContent = getSession(terminalId)?.cwd || '';
        const headerModel = document.createElement('span');
        headerModel.className = 'chat-header-model';
        const modeBtn = document.createElement('button');
        modeBtn.type = 'button';
        modeBtn.className = 'chat-header-mode';
        modeBtn.textContent = 'Switch to terminal';
        modeBtn.dataset.action = 'mode-switch'; // handled by tabs.js's delegated handler
        modeBtn.dataset.terminalId = terminalId;
        modeBtn.dataset.targetKind = 'terminal';
        const killBtn = document.createElement('button');
        killBtn.type = 'button';
        killBtn.className = 'chat-header-kill';
        killBtn.textContent = 'Kill';
        killBtn.dataset.action = 'kill-cleanup'; // reuses tabs.js's delegated cleanup handler
        killBtn.dataset.terminalId = terminalId;
        header.appendChild(headerCwd);
        header.appendChild(headerModel);
        header.appendChild(modeBtn);
        header.appendChild(killBtn);

        const loadMoreBtn = document.createElement('button');
        loadMoreBtn.type = 'button';
        loadMoreBtn.className = 'chat-load-more hidden';
        loadMoreBtn.textContent = 'Load earlier messages';

        // listWrap is a non-scrolling positioning context; the list scrolls
        // inside it and the jump button is pinned to the wrap (an absolute
        // button inside the scroll container itself scrolls out of view).
        const listWrap = document.createElement('div');
        listWrap.className = 'chat-list-wrap';
        const list = document.createElement('div');
        list.className = 'chat-message-list';
        // flow is the render target; list is the scroll container. The split
        // exists so chat-scroll.js can observe content size independently.
        const flow = document.createElement('div');
        flow.className = 'chat-message-flow';
        list.appendChild(flow);
        listWrap.appendChild(list);

        // Jump-to-latest: pinned bottom-right of the list viewport, shown only
        // while the user has scrolled away from the bottom (follow disengaged).
        const jumpBtn = document.createElement('button');
        jumpBtn.type = 'button';
        jumpBtn.className = 'chat-jump-latest hidden';
        jumpBtn.title = 'Jump to latest';
        jumpBtn.setAttribute('aria-label', 'Jump to latest');
        jumpBtn.textContent = '↓';
        listWrap.appendChild(jumpBtn);

        const instance = {
            root,
            list,
            flow,
            sticky: createStickyScroll(list, flow, {
                onFollowChange: (following) => jumpBtn.classList.toggle('hidden', following),
            }),
            headerCwd,
            headerModel,
            loadMoreBtn,
            state: createChatState(),
            transcriptLines: [],
            transcriptStart: 0,
            socket: null,
        };

        jumpBtn.addEventListener('click', () => instance.sticky.engage());
        loadMoreBtn.addEventListener('click', () => this._renderMoreHistory(terminalId));
        // Expand/collapse toggles change content height by user action, not
        // output — suppress the follow pin for that resize burst.
        list.addEventListener('click', (e) => {
            if (e.target.closest?.('.chat-tool-toggle, .chat-thinking-toggle')) instance.sticky.suppressNext();
        });
        killBtn.addEventListener('click', () => {
            fetch(sessionPath(terminalId), { method: 'DELETE' }).catch(() => {});
        });
        const inputBar = createInputBar({
            terminalId,
            onSend: (text, imagePath) => this._sendUserText(terminalId, text, imagePath),
            onStop: () => {
                // Interrupt the in-flight turn (verified control_request over
                // stream-json); the engine emits a result, which flips running
                // back off. Optimistically flip now so the button responds.
                instance.socket?.sendControl({
                    type: 'control_request',
                    request_id: 'int-' + Date.now(),
                    request: { subtype: 'interrupt' },
                });
                clearPending(instance.flow);
                this._setRunning(terminalId, false);
            },
        });
        instance.focusInput = inputBar.focus;
        instance.setRunning = inputBar.setRunning;

        root.appendChild(header);
        root.appendChild(loadMoreBtn);
        root.appendChild(listWrap);
        root.appendChild(inputBar.el);
        containerEl.appendChild(root);

        this.instances[terminalId] = instance;

        this._loadTranscript(terminalId).then(() => this._connect(terminalId));

        return instance;
    },

    // _setRunning flips the input bar between Send and Stop. Idempotent, and
    // safe if the instance or input bar isn't ready yet.
    _setRunning(terminalId, on) {
        this.instances[terminalId]?.setRunning?.(on);
    },

    // _sendUserText is the single send path (input bar and quick-reply chips):
    // optimistic echo, then branch on whether the write actually left. A send
    // while the socket isn't open echoes the bubble as UNSENT (retry offered)
    // and re-arms the connection — never a bubble that looks sent but wasn't.
    _sendUserText(terminalId, text, imagePath) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        if (!text && !imagePath) return;
        const sent = instance.socket?.sendControl(composeUserInput(text, imagePath)) === true;
        appendUserMessage(instance.flow, text, !!imagePath, Date.now(),
            sent ? null : { unsent: true, onRetry: () => this._sendUserText(terminalId, text, imagePath) });
        instance.sticky.engage();
        if (sent) {
            showPending(instance.flow);
            this._setRunning(terminalId, true);
        } else {
            instance.socket?.retry(); // re-arm a lost connection so a retry can land
        }
    },

    _connect(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        const { flow, headerCwd, headerModel } = instance;
        const socket = new SessionSocket(terminalId, {
            onControl: (evt) => {
                // Running-state edges from the stream: any turn activity marks
                // running (covers a viewer attaching mid-turn); a result ends
                // it. A mirrored co-viewer send (plain user event) also starts
                // one. control_response (interrupt ack) is not turn activity.
                if (evt.type === 'result') {
                    clearPending(flow); // turn ended (success or interrupt) — drop the thinking row
                    this._setRunning(terminalId, false);
                } else if (evt.type === 'stream_event' || evt.type === 'assistant') {
                    this._setRunning(terminalId, true); // turn activity (covers attaching mid-turn)
                }
                const patches = applyEvent(instance.state, evt);
                applyPatches(flow, patches, {
                    onHeader: (h) => { headerCwd.textContent = h.cwd; headerModel.textContent = h.model; },
                    onQuickReply: (text) => this._sendUserText(terminalId, text, null),
                });
            },
            onStatus: (status) => {
                clearPending(flow); // whatever happened, the turn is no longer just pending
                if (status !== 'open') this._setRunning(terminalId, false);
                if (status === 'ended') appendSystemNotice(flow, '[Session ended]');
                else if (status === 'reconnecting') appendSystemNotice(flow, '[Reconnecting…]');
                else if (status === 'lost') appendSystemNotice(flow, '[Connection lost — send a message to retry]');
            },
        });
        instance.socket = socket;
        socket.connect();
    },

    // _loadTranscript fetches the newest window of the conversation transcript
    // (server-side tail — §7 tail-first; the server bounds transfer so a huge
    // conversation never ships whole) and renders it.
    async _loadTranscript(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        const page = await this._fetchTranscriptPage(terminalId, '?tail=' + TAIL_INITIAL_LINES);
        instance.transcriptLines = page.lines;
        instance.transcriptStart = page.offset;
        this._renderBuffer(terminalId);
    },

    // _fetchTranscriptPage fetches one TranscriptPage; failures degrade to an
    // empty page (no history renders, live events still flow).
    async _fetchTranscriptPage(terminalId, query) {
        try {
            const res = await fetch(sessionTranscriptPath(terminalId) + query);
            if (res.ok) {
                const page = await res.json();
                if (Array.isArray(page.lines)) return { lines: page.lines, offset: page.offset || 0 };
            }
        } catch (e) { /* best-effort */ }
        return { lines: [], offset: 0 };
    },

    // _renderBuffer clears and replays the whole buffered window — a full
    // rebuild (not an incremental insert) so "load earlier" never has to
    // reason about DOM insertion order.
    _renderBuffer(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        resetView(instance.flow);
        instance.state = createChatState();
        for (let i = 0; i < instance.transcriptLines.length; i++) {
            let evt;
            try { evt = JSON.parse(instance.transcriptLines[i]); } catch (e) { continue; }
            // Init events aren't persisted, so replay recovers the model from
            // assistant records instead ('<synthetic>' = engine notices, skip).
            const model = evt.type === 'assistant' ? evt.message?.model : null;
            if (model && model !== '<synthetic>') instance.headerModel.textContent = model;
            // Replay flows through the same event classifier as live (user
            // turns → bubbles, tool_results → attached, the interrupt marker →
            // its Stopped line), so history and live render identically.
            const patches = applyEvent(instance.state, evt, { replay: true });
            applyPatches(instance.flow, patches, {
                onHeader: (h) => { instance.headerCwd.textContent = h.cwd; instance.headerModel.textContent = h.model; },
                onQuickReply: (text) => this._sendUserText(terminalId, text, null),
            });
        }
        instance.loadMoreBtn.classList.toggle('hidden', instance.transcriptStart === 0);
    },

    async _renderMoreHistory(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance || instance.transcriptStart === 0) return;
        const page = await this._fetchTranscriptPage(
            terminalId, '?before=' + instance.transcriptStart + '&count=' + TAIL_CHUNK);
        if (!page.lines.length) return;
        instance.transcriptLines = page.lines.concat(instance.transcriptLines);
        instance.transcriptStart = page.offset;
        // Reading earlier history: don't let content-growth re-pins snap the
        // list back to the bottom.
        instance.sticky.disengage();
        this._renderBuffer(terminalId);
    },

    destroy(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        instance.socket?.close();
        instance.sticky.destroy();
        delete this.instances[terminalId];
    },

    get(terminalId) {
        return this.instances[terminalId] || null;
    },

    focus(terminalId) {
        this.instances[terminalId]?.focusInput?.();
    },
};

// requestModeSwitch kills terminalId and respawns its conversation as
// targetKind, returning the new session name (or null on failure). Called
// from both surfaces' mode-switch buttons via tabs.js's delegated
// 'mode-switch' action handler, which opens the resulting session.
// init resets ChatManager's instance map (module-level state) so tests get a
// clean slate; there are no window-level listeners to install for chat (each
// instance owns its own SessionSocket and DOM, unlike terminal.js's shared
// resize/viewport handling).
export function init() {
    ChatManager.instances = {};
}

export async function requestModeSwitch(terminalId, targetKind) {
    try {
        const res = await fetch(sessionModePath(terminalId), {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ kind: targetKind }),
        });
        if (!res.ok) return null;
        const data = await res.json();
        return data.session_name || null;
    } catch (e) {
        return null;
    }
}
