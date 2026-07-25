// Chat surface: ChatView owns one chat session's DOM (header, message list,
// input bar) + its SessionSocket; ChatManager is the factory + registry keyed
// by terminalId (create/get/destroy/focus), mirroring TerminalManager so
// tabs.js treats either surface uniformly.

import { SessionSocket } from './session-socket.js';
import { createChatState, applyEvent, composeUserInput } from './chat-events.js';
import { applyPatches, appendUserMessage, appendSystemNotice, setConnectionNotice, clearConnectionNotice, resetView, showPending, clearPending } from './chat-render.js';
import { createStickyScroll } from './chat-scroll.js';
import { createInputBar } from './chat-input.js';
import { sessionTranscriptPath, sessionPath, sessionModePath } from './routes.js';
import { getSession } from './store.js';

// TAIL_INITIAL_LINES/TAIL_CHUNK bound how much transcript history renders
// eagerly (§7 of the chat-sessions design: tail-first, lazy-load older turns).
const TAIL_INITIAL_LINES = 200;
const TAIL_CHUNK = 200;

// ChatView owns one chat session's surface. The constructor builds the DOM and
// wires controls; start() loads transcript history then connects the live
// socket. this.destroyed guards async transcript loads against a torn-down view.
export class ChatView {
    constructor(terminalId, containerEl) {
        this.terminalId = terminalId;
        this.destroyed = false;

        containerEl.innerHTML = '';
        const root = this.root = document.createElement('div');
        root.className = 'chat-surface';

        const header = document.createElement('div');
        header.className = 'chat-header';
        const headerCwd = this.headerCwd = document.createElement('span');
        headerCwd.className = 'chat-header-cwd';
        // The live system/init event fires only at process start, so a viewer
        // joining later would see an empty header — seed cwd from the store.
        headerCwd.textContent = getSession(terminalId)?.cwd || '';
        const headerModel = this.headerModel = document.createElement('span');
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

        const loadMoreBtn = this.loadMoreBtn = document.createElement('button');
        loadMoreBtn.type = 'button';
        loadMoreBtn.className = 'chat-load-more hidden';
        loadMoreBtn.textContent = 'Load earlier messages';

        // listWrap is a non-scrolling positioning context; the list scrolls
        // inside it and the jump button is pinned to the wrap (an absolute
        // button inside the scroll container itself scrolls out of view).
        const listWrap = document.createElement('div');
        listWrap.className = 'chat-list-wrap';
        const list = this.list = document.createElement('div');
        list.className = 'chat-message-list';
        // flow is the render target; list is the scroll container. The split
        // exists so chat-scroll.js can observe content size independently.
        const flow = this.flow = document.createElement('div');
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

        this.state = createChatState();
        this.transcriptLines = [];
        this.transcriptStart = 0;
        this.socket = null;
        this.sticky = createStickyScroll(list, flow, {
            onFollowChange: (following) => jumpBtn.classList.toggle('hidden', following),
        });

        jumpBtn.addEventListener('click', () => this.sticky.engage());
        loadMoreBtn.addEventListener('click', () => this.renderMoreHistory());
        // Expand/collapse toggles change content height by user action, not
        // output — suppress the follow pin for that resize burst.
        list.addEventListener('click', (e) => {
            if (e.target.closest?.('.chat-tool-toggle, .chat-thinking-toggle')) this.sticky.suppressNext();
        });
        killBtn.addEventListener('click', () => {
            fetch(sessionPath(terminalId), { method: 'DELETE' }).catch(() => {});
        });
        const inputBar = createInputBar({
            terminalId,
            onSend: (text, imagePath) => this.sendUserText(text, imagePath),
            onStop: () => {
                // Interrupt the in-flight turn (verified control_request over
                // stream-json); the engine emits a result, which flips running
                // back off. Optimistically flip now so the button responds.
                this.socket?.sendControl({
                    type: 'control_request',
                    request_id: 'int-' + Date.now(),
                    request: { subtype: 'interrupt' },
                });
                clearPending(this.flow);
                this.setRunningState(false);
            },
        });
        this.focusInput = inputBar.focus;
        this.setRunning = inputBar.setRunning;

        root.appendChild(header);
        root.appendChild(loadMoreBtn);
        root.appendChild(listWrap);
        root.appendChild(inputBar.el);
        containerEl.appendChild(root);
    }

    start() {
        this.loadTranscript().then(() => this.connect());
    }

    // setRunningState flips the input bar between Send and Stop. Idempotent, and
    // safe if the input bar isn't ready yet.
    setRunningState(on) {
        this.setRunning?.(on);
        // Pulse the active turn's avatar while the turn runs (CSS) — continuous
        // "working" feedback from send to result, through every phase.
        this.flow?.classList.toggle('is-turn-running', on);
    }

    // sendUserText is the single send path (input bar and quick-reply chips):
    // optimistic echo, then branch on whether the write actually left. A send
    // while the socket isn't open echoes the bubble as UNSENT (retry offered)
    // and re-arms the connection — never a bubble that looks sent but wasn't.
    sendUserText(text, imagePath) {
        if (!text && !imagePath) return;
        const sent = this.socket?.sendControl(composeUserInput(text, imagePath)) === true;
        appendUserMessage(this.flow, text, !!imagePath, Date.now(),
            sent ? null : { unsent: true, onRetry: () => this.sendUserText(text, imagePath) });
        this.sticky.engage();
        if (sent) {
            showPending(this.flow);
            this.setRunningState(true);
        } else {
            this.socket?.retry(); // re-arm a lost connection so a retry can land
        }
    }

    connect() {
        const { flow, headerCwd, headerModel } = this;
        const socket = new SessionSocket(this.terminalId, {
            onControl: (evt) => {
                // Running-state edges from the stream: any turn activity marks
                // running (covers a viewer attaching mid-turn); a result ends
                // it. A mirrored co-viewer send (plain user event) also starts
                // one. control_response (interrupt ack) is not turn activity.
                if (evt.type === 'result') {
                    clearPending(flow); // turn ended (success or interrupt) — drop the thinking row
                    this.setRunningState(false);
                } else if (evt.type === 'stream_event' || evt.type === 'assistant') {
                    this.setRunningState(true); // turn activity (covers attaching mid-turn)
                }
                const patches = applyEvent(this.state, evt);
                applyPatches(flow, patches, {
                    onHeader: (h) => { headerCwd.textContent = h.cwd; headerModel.textContent = h.model; },
                    onQuickReply: (text) => this.sendUserText(text, null),
                });
            },
            onStatus: (status, info = {}) => {
                clearPending(flow); // whatever happened, the turn is no longer just pending
                if (status !== 'open') this.setRunningState(false);
                if (status === 'open') clearConnectionNotice(flow); // reconnected: drop the transient notice
                else if (status === 'reconnecting') setConnectionNotice(flow, `[Reconnecting… (attempt ${info.attempt})]`);
                else if (status === 'lost') setConnectionNotice(flow, '[Connection lost — send a message to retry]');
                else if (status === 'ended') { clearConnectionNotice(flow); appendSystemNotice(flow, '[Session ended]'); }
            },
        });
        this.socket = socket;
        socket.connect();
    }

    // loadTranscript fetches the newest window of the conversation transcript
    // (server-side tail — §7 tail-first; the server bounds transfer so a huge
    // conversation never ships whole) and renders it.
    async loadTranscript() {
        const page = await this.fetchTranscriptPage('?tail=' + TAIL_INITIAL_LINES);
        if (this.destroyed) return;
        this.transcriptLines = page.lines;
        this.transcriptStart = page.offset;
        this.renderBuffer();
    }

    // fetchTranscriptPage fetches one TranscriptPage; failures degrade to an
    // empty page (no history renders, live events still flow).
    async fetchTranscriptPage(query) {
        try {
            const res = await fetch(sessionTranscriptPath(this.terminalId) + query);
            if (res.ok) {
                const page = await res.json();
                if (Array.isArray(page.lines)) return { lines: page.lines, offset: page.offset || 0 };
            }
        } catch (e) { /* best-effort */ }
        return { lines: [], offset: 0 };
    }

    // renderBuffer clears and replays the whole buffered window — a full rebuild
    // (not an incremental insert) so "load earlier" never has to reason about
    // DOM insertion order.
    renderBuffer() {
        if (this.destroyed) return;
        resetView(this.flow);
        this.state = createChatState();
        for (let i = 0; i < this.transcriptLines.length; i++) {
            let evt;
            try { evt = JSON.parse(this.transcriptLines[i]); } catch (e) { continue; }
            // Init events aren't persisted, so replay recovers the model from
            // assistant records instead ('<synthetic>' = engine notices, skip).
            const model = evt.type === 'assistant' ? evt.message?.model : null;
            if (model && model !== '<synthetic>') this.headerModel.textContent = model;
            // Replay flows through the same event classifier as live (user
            // turns → bubbles, tool_results → attached, the interrupt marker →
            // its Stopped line), so history and live render identically.
            const patches = applyEvent(this.state, evt, { replay: true });
            applyPatches(this.flow, patches, {
                onHeader: (h) => { this.headerCwd.textContent = h.cwd; this.headerModel.textContent = h.model; },
                onQuickReply: (text) => this.sendUserText(text, null),
            });
        }
        this.loadMoreBtn.classList.toggle('hidden', this.transcriptStart === 0);
    }

    async renderMoreHistory() {
        if (this.transcriptStart === 0) return;
        const page = await this.fetchTranscriptPage(
            '?before=' + this.transcriptStart + '&count=' + TAIL_CHUNK);
        if (this.destroyed || !page.lines.length) return;
        this.transcriptLines = page.lines.concat(this.transcriptLines);
        this.transcriptStart = page.offset;
        // Reading earlier history: don't let content-growth re-pins snap the
        // list back to the bottom.
        this.sticky.disengage();
        this.renderBuffer();
    }

    destroy() {
        this.destroyed = true;
        this.socket?.close();
        this.sticky.destroy();
    }

    focus() {
        this.focusInput?.();
    }
}

// ChatManager — factory + registry for the live ChatView instances, keyed by
// terminalId.
export const ChatManager = {
    instances: {}, // terminalId -> ChatView

    create(terminalId, containerEl) {
        if (this.instances[terminalId]) this.destroy(terminalId);
        const view = new ChatView(terminalId, containerEl);
        this.instances[terminalId] = view;
        view.start();
        return view;
    },

    destroy(terminalId) {
        const view = this.instances[terminalId];
        if (!view) return;
        view.destroy();
        delete this.instances[terminalId];
    },

    get(terminalId) {
        return this.instances[terminalId] || null;
    },

    focus(terminalId) {
        this.instances[terminalId]?.focus();
    },
};

// init resets ChatManager's instance map (module-level state) so tests get a
// clean slate; there are no window-level listeners to install for chat (each
// instance owns its own SessionSocket and DOM, unlike terminal.js's shared
// resize/viewport handling).
export function init() {
    ChatManager.instances = {};
}

// requestModeSwitch kills terminalId and respawns its conversation as
// targetKind, returning the new session name (or null on failure). Called from
// both surfaces' mode-switch buttons via tabs.js's delegated 'mode-switch'
// action handler, which opens the resulting session.
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
