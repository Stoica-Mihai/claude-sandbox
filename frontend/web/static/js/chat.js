// Chat surface manager: owns one chat session's DOM (header, message list,
// input bar) and its SessionSocket, mirroring TerminalManager's shape
// (create/destroy/get) so tabs.js can treat either surface uniformly.

import { SessionSocket } from './session-socket.js';
import { createChatState, applyEvent, composeUserInput, transcriptUserText } from './chat-events.js';
import { applyPatches, appendUserMessage, appendSystemNotice } from './chat-render.js';
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
        const headerCost = document.createElement('span');
        headerCost.className = 'chat-header-cost';
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
        header.appendChild(headerCost);
        header.appendChild(modeBtn);
        header.appendChild(killBtn);

        const loadMoreBtn = document.createElement('button');
        loadMoreBtn.type = 'button';
        loadMoreBtn.className = 'chat-load-more hidden';
        loadMoreBtn.textContent = 'Load earlier messages';

        const list = document.createElement('div');
        list.className = 'chat-message-list';

        const instance = {
            root,
            list,
            headerCwd,
            headerModel,
            headerCost,
            loadMoreBtn,
            state: createChatState(),
            transcriptLines: [],
            transcriptRenderedFrom: 0,
            socket: null,
        };

        loadMoreBtn.addEventListener('click', () => this._renderMoreHistory(terminalId));
        killBtn.addEventListener('click', () => {
            fetch(sessionPath(terminalId), { method: 'DELETE' }).catch(() => {});
        });
        const inputBar = createInputBar({
            terminalId,
            onSend: (text, imagePath) => {
                if (instance.socket?.status === 'lost') {
                    instance.socket.retry();
                    return;
                }
                if (!text && !imagePath) return;
                appendUserMessage(list, imagePath ? (text ? text + ' 📎' : '📎 image attached') : text);
                instance.socket?.sendControl(composeUserInput(text, imagePath));
            },
        });
        instance.focusInput = inputBar.focus;

        root.appendChild(header);
        root.appendChild(loadMoreBtn);
        root.appendChild(list);
        root.appendChild(inputBar.el);
        containerEl.appendChild(root);

        this.instances[terminalId] = instance;

        this._loadTranscript(terminalId).then(() => this._connect(terminalId));

        return instance;
    },

    _connect(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        const { list, headerCwd, headerModel, headerCost } = instance;
        const socket = new SessionSocket(terminalId, {
            onControl: (evt) => {
                const patches = applyEvent(instance.state, evt);
                applyPatches(list, patches, {
                    onHeader: (h) => { headerCwd.textContent = h.cwd; headerModel.textContent = h.model; },
                    onUsage: (u) => { headerCost.textContent = u.totalCostUsd != null ? '$' + u.totalCostUsd.toFixed(4) : ''; },
                });
            },
            onStatus: (status) => {
                if (status === 'ended') appendSystemNotice(list, '[Session ended]');
                else if (status === 'reconnecting') appendSystemNotice(list, '[Reconnecting…]');
                else if (status === 'lost') appendSystemNotice(list, '[Connection lost — send a message to retry]');
            },
        });
        instance.socket = socket;
        socket.connect();
    },

    // _loadTranscript fetches the conversation transcript and renders its tail
    // (§7: tail-first, no chat-side snapshot machinery — history comes from
    // the transcript, live events from the stream).
    async _loadTranscript(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        let lines = [];
        try {
            const res = await fetch(sessionTranscriptPath(terminalId));
            if (res.ok) {
                const text = await res.text();
                lines = text.split('\n').filter(Boolean);
            }
        } catch (e) { /* best-effort — a fetch failure just means no history renders */ }
        instance.transcriptLines = lines;
        const from = Math.max(0, lines.length - TAIL_INITIAL_LINES);
        this._rebuildFromTranscript(terminalId, from);
    },

    // _rebuildFromTranscript clears and replays the message list from a given
    // transcript line index — a full rebuild (not an incremental insert) so
    // "load earlier" never has to reason about DOM insertion order.
    _rebuildFromTranscript(terminalId, from) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        instance.list.innerHTML = '';
        instance.state = createChatState();
        instance.transcriptRenderedFrom = from;
        for (let i = from; i < instance.transcriptLines.length; i++) {
            let evt;
            try { evt = JSON.parse(instance.transcriptLines[i]); } catch (e) { continue; }
            // Init events aren't persisted, so replay recovers the model from
            // assistant records instead ('<synthetic>' = engine notices, skip).
            const model = evt.type === 'assistant' ? evt.message?.model : null;
            if (model && model !== '<synthetic>') instance.headerModel.textContent = model;
            // Plain user turns exist only in the transcript (live, the input bar
            // echoes them locally and the stream never carries them back).
            const userText = transcriptUserText(evt);
            if (userText !== null) {
                appendUserMessage(instance.list, userText);
                continue;
            }
            const patches = applyEvent(instance.state, evt, { replay: true });
            applyPatches(instance.list, patches, {
                onHeader: (h) => { instance.headerCwd.textContent = h.cwd; instance.headerModel.textContent = h.model; },
                onUsage: (u) => { instance.headerCost.textContent = u.totalCostUsd != null ? '$' + u.totalCostUsd.toFixed(4) : ''; },
            });
        }
        instance.loadMoreBtn.classList.toggle('hidden', from === 0);
    },

    _renderMoreHistory(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        const from = Math.max(0, instance.transcriptRenderedFrom - TAIL_CHUNK);
        // Reading earlier history: don't let the rebuild's renders snap the
        // list back to the bottom.
        instance.list._chatFollow = false;
        this._rebuildFromTranscript(terminalId, from);
    },

    destroy(terminalId) {
        const instance = this.instances[terminalId];
        if (!instance) return;
        instance.socket?.close();
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
