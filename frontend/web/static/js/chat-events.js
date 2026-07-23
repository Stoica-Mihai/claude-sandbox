// Pure stream-json event → render-patch translation. No DOM here — this
// module owns the chat state model and turns one incoming event object into
// zero or more patches for chat-render.js to apply. Kept DOM-free so the
// event vocabulary mapping is unit-testable without a browser.
//
// Verified against the pinned engine (2026-07-22, 2.1.215): system/init,
// stream_event wrapping the Anthropic SSE vocabulary (message_start,
// content_block_start/delta/stop), assistant (full message), result,
// conversation_reset. Tool-use/tool-result/thinking block shapes follow the
// documented Anthropic Messages API vocabulary (stable, used by every Claude
// client) but were not directly re-probed for a tool-using turn — the P1 spike
// (design doc §13) is the place to close that gap with a live probe.

// createChatState returns a fresh, empty conversation state.
export function createChatState() {
    return {
        header: { cwd: '', model: '', sessionId: '' },
        usage: { totalCostUsd: null, inputTokens: null, outputTokens: null },
        messages: [], // { id, role, blocks: [ {type,text,collapsed,toolName,toolUseId,input,inputRaw,result,resultText,done} ] }
        // blockIndex tracks the open assistant message's content-block-index → block, for
        // stream_event content_block_* dispatch while a message is in progress.
        _openMessageId: null,
        _openBlocks: null,
    };
}

function newBlock(type, extra) {
    return Object.assign({ type, text: '', done: false }, extra || {});
}

function findMessage(state, id) {
    return state.messages.find(m => m.id === id) || null;
}

// applyEvent mutates state and returns a list of patches describing what
// changed, for the renderer to apply. Unrecognized event types produce no
// patches (forward-compatible: an unknown event is silently ignored, matching
// the "frontend owns the protocol brain but is not exhaustive" design stance).
export function applyEvent(state, evt, opts) {
    if (!evt || typeof evt !== 'object') return [];

    switch (evt.type) {
        case 'system':
            return applySystem(state, evt);
        case 'stream_event':
            return applyStreamEvent(state, evt);
        case 'assistant':
            return applyFullMessage(state, evt, 'assistant', !!opts?.replay);
        case 'user':
            return applyUserEvent(state, evt);
        case 'result':
            return applyResult(state, evt);
        case 'conversation_reset':
            return applyReset(state, evt);
        default:
            return [];
    }
}

function applySystem(state, evt) {
    if (evt.subtype !== 'init') return [];
    state.header = { cwd: evt.cwd || '', model: evt.model || '', sessionId: evt.session_id || '' };
    return [{ kind: 'header', header: state.header }];
}

function applyStreamEvent(state, evt) {
    const e = evt.event;
    if (!e || typeof e !== 'object') return [];
    switch (e.type) {
        case 'message_start': {
            const id = e.message?.id || ('msg-' + state.messages.length);
            const msg = { id, role: 'assistant', blocks: [] };
            state.messages.push(msg);
            state._openMessageId = id;
            state._openBlocks = [];
            return [{ kind: 'new-message', messageId: id, role: 'assistant' }];
        }
        case 'content_block_start': {
            const msg = findMessage(state, state._openMessageId);
            if (!msg) return [];
            const cb = e.content_block || {};
            let block;
            if (cb.type === 'tool_use') {
                block = newBlock('tool', { toolName: cb.name || '', toolUseId: cb.id || '', inputRaw: '' });
            } else if (cb.type === 'thinking') {
                block = newBlock('thinking', { collapsed: true });
            } else {
                block = newBlock('text', { text: cb.text || '' });
            }
            msg.blocks[e.index] = block;
            return [{ kind: 'new-block', messageId: msg.id, blockIndex: e.index, block }];
        }
        case 'content_block_delta': {
            const msg = findMessage(state, state._openMessageId);
            if (!msg) return [];
            const block = msg.blocks[e.index];
            if (!block) return [];
            const d = e.delta || {};
            if (d.type === 'text_delta' && typeof d.text === 'string') {
                block.text += d.text;
                return [{ kind: 'append-text', messageId: msg.id, blockIndex: e.index, block }];
            }
            if (d.type === 'thinking_delta' && typeof d.thinking === 'string') {
                block.text += d.thinking;
                return [{ kind: 'append-text', messageId: msg.id, blockIndex: e.index, block }];
            }
            if (d.type === 'input_json_delta' && typeof d.partial_json === 'string') {
                block.inputRaw += d.partial_json;
                return [{ kind: 'tool-input-progress', messageId: msg.id, blockIndex: e.index, block }];
            }
            return [];
        }
        case 'content_block_stop': {
            const msg = findMessage(state, state._openMessageId);
            if (!msg) return [];
            const block = msg.blocks[e.index];
            if (!block) return [];
            block.done = true;
            if (block.type === 'tool' && block.inputRaw) {
                try { block.input = JSON.parse(block.inputRaw); } catch (err) { /* keep raw text */ }
            }
            return [{ kind: 'finalize-block', messageId: msg.id, blockIndex: e.index, block }];
        }
        case 'message_stop': {
            const messageId = state._openMessageId;
            state._openMessageId = null;
            state._openBlocks = null;
            return messageId ? [{ kind: 'finalize-message', messageId }] : [];
        }
        default:
            return [];
    }
}

// applyFullMessage handles the authoritative full "assistant" event — it may
// arrive alongside streaming deltas (verifying/completing them) or, for a
// non-streamed turn, be the only signal. Blocks already rendered via deltas
// are left as-is (same id, already finalized); any block missing from the
// streamed state is added so nothing is lost if a delta was missed.
// Replay differs: transcripts fragment one message into several records that
// share message.id, each carrying one block at index 0 — so replay APPENDS
// blocks instead of index-matching (which would drop every record after the
// first).
function applyFullMessage(state, evt, role, replay) {
    const msg = evt.message;
    if (!msg || !msg.id) return [];
    let existing = findMessage(state, msg.id);
    const patches = [];
    if (!existing) {
        existing = { id: msg.id, role, blocks: [] };
        state.messages.push(existing);
        patches.push({ kind: 'new-message', messageId: msg.id, role });
    }
    (msg.content || []).forEach((cb, i) => {
        // Transcripts persist thinking blocks with empty text (shell only) —
        // nothing to show, so don't render a dead toggle.
        if (cb.type === 'thinking' && !cb.thinking) return;
        if (replay) i = existing.blocks.length;
        else if (existing.blocks[i] && existing.blocks[i].done) return; // already rendered via deltas
        let block;
        if (cb.type === 'tool_use') {
            block = newBlock('tool', { toolName: cb.name || '', toolUseId: cb.id || '', input: cb.input, done: true });
        } else if (cb.type === 'thinking') {
            block = newBlock('thinking', { text: cb.thinking || '', collapsed: true, done: true });
        } else {
            block = newBlock('text', { text: cb.text || '', done: true });
        }
        existing.blocks[i] = block;
        patches.push({ kind: 'new-block', messageId: existing.id, blockIndex: i, block });
        patches.push({ kind: 'finalize-block', messageId: existing.id, blockIndex: i, block });
    });
    patches.push({ kind: 'finalize-message', messageId: existing.id });
    return patches;
}

// applyUserEvent handles the "user" top-level event: tool_result blocks are
// matched to their tool call by tool_use_id. Plain user text on the live
// stream can only be ANOTHER viewer's send — the engine never echoes user
// turns, sessiond mirrors them to co-viewers only, and the sender renders
// its own message locally (see chat-input.js) — so it renders as a bubble.
function applyUserEvent(state, evt) {
    const content = evt.message?.content;
    if (!Array.isArray(content)) return [];
    const patches = [];
    for (const cb of content) {
        if (cb.type !== 'tool_result') continue;
        const found = findToolBlock(state, cb.tool_use_id);
        if (!found) continue;
        found.block.result = cb.content;
        found.block.resultText = toolResultText(cb.content);
        patches.push({ kind: 'tool-result', messageId: found.messageId, blockIndex: found.blockIndex, block: found.block });
    }
    if (patches.length) return patches;
    const text = transcriptUserText(evt);
    if (text === null) return patches;
    // The engine emits this synthetic user turn right after an interrupt; it
    // is an abort marker, not a real send — render it as a system notice so
    // it neither starts a new turn nor shows a pending row.
    if (text === INTERRUPT_MARKER) return [{ kind: 'interrupt' }];
    return [{ kind: 'user-message', text }];
}

const INTERRUPT_MARKER = '[Request interrupted by user]';

function findToolBlock(state, toolUseId) {
    if (!toolUseId) return null;
    for (const msg of state.messages) {
        for (let i = 0; i < msg.blocks.length; i++) {
            const b = msg.blocks[i];
            if (b && b.type === 'tool' && b.toolUseId === toolUseId) {
                return { messageId: msg.id, blockIndex: i, block: b };
            }
        }
    }
    return null;
}

// toolResultText renders a tool_result's content (string or content-block
// array) as a plain excerpt string.
function toolResultText(content) {
    if (typeof content === 'string') return content;
    if (Array.isArray(content)) {
        return content.map(c => (typeof c === 'string' ? c : c.text || '')).join('\n');
    }
    return '';
}

function applyResult(state, evt) {
    state.usage = {
        totalCostUsd: evt.total_cost_usd ?? state.usage.totalCostUsd,
        inputTokens: evt.usage?.input_tokens ?? state.usage.inputTokens,
        outputTokens: evt.usage?.output_tokens ?? state.usage.outputTokens,
    };
    return [{ kind: 'usage', usage: state.usage }];
}

function applyReset(state, evt) {
    return [{ kind: 'system-notice', text: '/clear — conversation context cleared' }];
}

// transcriptUserText extracts the text of a plain user turn from a transcript
// record, or null when the record is not one (meta records, tool_result
// carriers). Replay-only: live user events carry only tool_results.
export function transcriptUserText(evt) {
    if (!evt || evt.type !== 'user' || evt.isMeta) return null;
    const content = evt.message?.content;
    if (typeof content === 'string') return content || null;
    if (!Array.isArray(content)) return null;
    if (content.some((cb) => cb.type === 'tool_result')) return null;
    let text = content.filter((cb) => cb.type === 'text').map((cb) => cb.text || '').join('\n');
    // Render attachment markers the way the live echo does (chat.js): 📎, not
    // the raw upload path composeUserInput embeds. Matches both the current
    // "file" wording and the older "image" wording in existing transcripts.
    text = text.replace(/\s*\[Attached (?:file|image): [^\]]*\]/g, ' 📎').trim();
    return text || null;
}

// composeUserInput builds the outbound stream-json user message. filePath,
// when set, references an already-uploaded file by path (see chat-input.js —
// the model Reads the file itself); inline file bytes are never sent.
export function composeUserInput(text, filePath) {
    let combined = text || '';
    if (filePath) {
        combined = combined ? combined + '\n\n[Attached file: ' + filePath + ']' : '[Attached file: ' + filePath + ']';
    }
    return { type: 'user', message: { role: 'user', content: [{ type: 'text', text: combined }] } };
}
