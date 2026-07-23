// Pure stream-json event → patch translation tests (no DOM).
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { createChatState, applyEvent, composeUserInput } from '../chat-events.js';

test('system/init patches the header', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'system', subtype: 'init', cwd: '/workspace/x', model: 'claude-opus-4-8', session_id: 'sid-1' });
    assert.equal(patches.length, 1);
    assert.equal(patches[0].kind, 'header');
    assert.deepEqual(state.header, { cwd: '/workspace/x', model: 'claude-opus-4-8', sessionId: 'sid-1' });
});

test('system with a non-init subtype produces no patches', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'system', subtype: 'status', status: 'busy' });
    assert.deepEqual(patches, []);
});

test('streaming text accumulates incrementally across content_block_delta events', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-1' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'Hel' } } });
    const patches = applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'lo' } } });

    const msg = state.messages.find(m => m.id === 'msg-1');
    assert.equal(msg.blocks[0].text, 'Hello');
    assert.equal(patches[0].kind, 'append-text');
});

test('content_block_stop finalizes the block', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-1' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } } });
    const patches = applyEvent(state, { type: 'stream_event', event: { type: 'content_block_stop', index: 0 } });
    const msg = state.messages.find(m => m.id === 'msg-1');
    assert.equal(msg.blocks[0].done, true);
    assert.equal(patches[0].kind, 'finalize-block');
});

test('message_stop closes the open message', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-1' } } });
    const patches = applyEvent(state, { type: 'stream_event', event: { type: 'message_stop' } });
    assert.equal(state._openMessageId, null);
    assert.equal(patches[0].kind, 'finalize-message');
});

test('thinking block accumulates and defaults to collapsed', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-1' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'thinking' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'thinking_delta', thinking: 'reasoning...' } } });
    const msg = state.messages.find(m => m.id === 'msg-1');
    assert.equal(msg.blocks[0].type, 'thinking');
    assert.equal(msg.blocks[0].collapsed, true);
    assert.equal(msg.blocks[0].text, 'reasoning...');
});

test('tool_use block accumulates partial_json and parses it on stop', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-1' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'tool_use', id: 'tool-1', name: 'Bash' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'input_json_delta', partial_json: '{"command":' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'input_json_delta', partial_json: '"ls -la"}' } } });
    const patches = applyEvent(state, { type: 'stream_event', event: { type: 'content_block_stop', index: 0 } });

    const msg = state.messages.find(m => m.id === 'msg-1');
    const block = msg.blocks[0];
    assert.equal(block.type, 'tool');
    assert.equal(block.toolUseId, 'tool-1');
    assert.deepEqual(block.input, { command: 'ls -la' });
    assert.equal(patches[0].kind, 'finalize-block');
});

test('tool_use with unparseable input keeps the raw text without throwing', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-1' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'tool_use', id: 'tool-1', name: 'Bash' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'input_json_delta', partial_json: 'not json' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_stop', index: 0 } });

    const msg = state.messages.find(m => m.id === 'msg-1');
    assert.equal(msg.blocks[0].input, undefined);
    assert.equal(msg.blocks[0].inputRaw, 'not json');
});

test('user event with a tool_result attaches it to the matching tool block by id', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-1' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'tool_use', id: 'tool-1', name: 'Bash' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_stop', index: 0 } });

    const patches = applyEvent(state, { type: 'user', message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'tool-1', content: 'total 0\n' }] } });

    const msg = state.messages.find(m => m.id === 'msg-1');
    assert.equal(msg.blocks[0].resultText, 'total 0\n');
    assert.equal(patches[0].kind, 'tool-result');
});

test('user event with plain text renders a bubble (sessiond mirrors co-viewer sends; the sender never receives its own)', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hello' }] } });
    assert.deepEqual(patches, [{ kind: 'user-message', text: 'hello', attachment: false }]);
});

test('user event with a tool_result for an unknown tool_use_id is ignored', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'nope', content: 'x' }] } });
    assert.deepEqual(patches, []);
});

test('assistant full-message event adds any block missing from streamed state', () => {
    const state = createChatState();
    // No stream_event deltas at all — the full "assistant" event is the only signal.
    const patches = applyEvent(state, {
        type: 'assistant',
        message: { id: 'msg-2', content: [{ type: 'text', text: 'full text' }] },
    });
    const msg = state.messages.find(m => m.id === 'msg-2');
    assert.equal(msg.blocks[0].text, 'full text');
    assert.equal(msg.blocks[0].done, true);
    assert.ok(patches.some(p => p.kind === 'new-block'));
    assert.ok(patches.some(p => p.kind === 'finalize-message'));
});

test('assistant full-message event does not clobber an already-finalized streamed block', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg-3' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'streamed' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_stop', index: 0 } });

    applyEvent(state, { type: 'assistant', message: { id: 'msg-3', content: [{ type: 'text', text: 'DIFFERENT (should be ignored)' }] } });

    const msg = state.messages.find(m => m.id === 'msg-3');
    assert.equal(msg.blocks[0].text, 'streamed');
});

test('result event captures usage/cost', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'result', total_cost_usd: 0.0123, usage: { input_tokens: 10, output_tokens: 20 } });
    assert.equal(patches[0].kind, 'usage');
    assert.deepEqual(state.usage, { totalCostUsd: 0.0123, inputTokens: 10, outputTokens: 20 });
});

test('conversation_reset produces a system notice', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'conversation_reset', session_id: 'old', new_conversation_id: 'unreliable' });
    assert.equal(patches.length, 1);
    assert.equal(patches[0].kind, 'system-notice');
});

test('unknown event types are silently ignored', () => {
    const state = createChatState();
    assert.deepEqual(applyEvent(state, { type: 'rate_limit_event', rate_limit_info: {} }), []);
    assert.deepEqual(applyEvent(state, {}), []);
    assert.deepEqual(applyEvent(state, null), []);
});

test('composeUserInput builds a plain text message with no image', () => {
    const msg = composeUserInput('hello', null);
    assert.equal(msg.type, 'user');
    assert.equal(msg.message.content[0].text, 'hello');
});

test('composeUserInput references the image path, never inline image bytes', () => {
    const msg = composeUserInput('check this', '/tmp/uploads/x/clipboard-abcd.png');
    const text = msg.message.content[0].text;
    assert.ok(text.includes('check this'));
    assert.ok(text.includes('/tmp/uploads/x/clipboard-abcd.png'));
    assert.equal(msg.message.content.length, 1);
    assert.ok(!JSON.stringify(msg).includes('base64'));
});

test('composeUserInput with only an image and no caption still sends a reference', () => {
    const msg = composeUserInput('', '/tmp/uploads/x/clipboard-abcd.png');
    assert.ok(msg.message.content[0].text.includes('/tmp/uploads/x/clipboard-abcd.png'));
});

test('transcriptUserText extracts plain user turns', async () => {
    const { transcriptUserText } = await import('../chat-events.js');
    assert.deepEqual(transcriptUserText({ type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hello' }] } }), { text: 'hello', hasAttachment: false });
    assert.deepEqual(transcriptUserText({ type: 'user', message: { role: 'user', content: 'plain string' } }), { text: 'plain string', hasAttachment: false });
});

test('transcriptUserText rejects non-user, meta, and tool_result records', async () => {
    const { transcriptUserText } = await import('../chat-events.js');
    assert.equal(transcriptUserText({ type: 'assistant', message: { content: [{ type: 'text', text: 'x' }] } }), null);
    assert.equal(transcriptUserText({ type: 'user', isMeta: true, message: { content: [{ type: 'text', text: 'x' }] } }), null);
    assert.equal(transcriptUserText({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 't1', content: 'ok' }] } }), null);
    assert.equal(transcriptUserText({ type: 'user', message: { content: [] } }), null);
    assert.equal(transcriptUserText(null), null);
});

test('replay appends fragmented same-id assistant records instead of index-dropping them', () => {
    const state = createChatState();
    const mk = (cb) => ({ type: 'assistant', message: { id: 'msg_A', content: [cb] } });
    applyEvent(state, mk({ type: 'thinking', thinking: 'hmm' }), { replay: true });
    applyEvent(state, mk({ type: 'text', text: 'the answer' }), { replay: true });
    applyEvent(state, mk({ type: 'tool_use', id: 't1', name: 'Read', input: { file_path: '/x' } }), { replay: true });
    assert.equal(state.messages.length, 1);
    assert.deepEqual(state.messages[0].blocks.map(b => b.type), ['thinking', 'text', 'tool']);
    assert.equal(state.messages[0].blocks[1].text, 'the answer');
});

test('live full assistant events still index-match streamed blocks (no replay flag)', () => {
    const state = createChatState();
    applyEvent(state, { type: 'stream_event', event: { type: 'message_start', message: { id: 'msg_B' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_start', index: 0, content_block: { type: 'text' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'streamed' } } });
    applyEvent(state, { type: 'stream_event', event: { type: 'content_block_stop', index: 0 } });
    applyEvent(state, { type: 'assistant', message: { id: 'msg_B', content: [{ type: 'text', text: 'streamed' }] } });
    assert.equal(state.messages.length, 1);
    assert.equal(state.messages[0].blocks.length, 1); // full event did not duplicate the streamed block
});

test('transcriptUserText strips the attachment marker and flags it (no emoji in text)', async () => {
    const { transcriptUserText } = await import('../chat-events.js');
    const evt = { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'did i attach something?\n\n[Attached image: /home/claude/.local/state/claude/uploads/x/y.jpg]' }] } };
    assert.deepEqual(transcriptUserText(evt), { text: 'did i attach something?', hasAttachment: true });
});

test('empty thinking shells (transcript form) produce no block', () => {
    const state = createChatState();
    applyEvent(state, { type: 'assistant', message: { id: 'msg_C', content: [{ type: 'thinking', thinking: '' }] } }, { replay: true });
    applyEvent(state, { type: 'assistant', message: { id: 'msg_C', content: [{ type: 'text', text: 'visible' }] } }, { replay: true });
    assert.deepEqual(state.messages[0].blocks.map(b => b.type), ['text']);
});

test('a mirrored plain-text user event renders as a user bubble patch', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'from the phone\n\n[Attached image: /up/x.jpg]' }] } });
    assert.deepEqual(patches, [{ kind: 'user-message', text: 'from the phone', attachment: true }]);
});

test('tool_result user events never produce a user bubble', () => {
    const state = createChatState();
    applyEvent(state, { type: 'assistant', message: { id: 'm1', content: [{ type: 'tool_use', id: 't1', name: 'Bash', input: {} }] } });
    const patches = applyEvent(state, { type: 'user', message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: 't1', content: 'out' }] } });
    assert.equal(patches.length, 1);
    assert.equal(patches[0].kind, 'tool-result');
});

test('the post-interrupt marker becomes an interrupt patch, not a user turn', () => {
    const state = createChatState();
    const patches = applyEvent(state, { type: 'user', message: { role: 'user', content: [{ type: 'text', text: '[Request interrupted by user]' }] } });
    assert.deepEqual(patches, [{ kind: 'interrupt' }]);
});

test('the "file" attachment marker is stripped and flagged too', () => {
    const evt = { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'see this\n\n[Attached file: /up/report.pdf]' }] } };
    const patches = applyEvent(createChatState(), evt);
    assert.deepEqual(patches, [{ kind: 'user-message', text: 'see this', attachment: true }]);
});
