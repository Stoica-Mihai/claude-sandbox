// DOM rendering for the chat message list: markdown, streaming text, tool
// step rows (diff for Edit/Write, command+output for Bash), collapsed
// thinking blocks, and system notices. Consumes the patches chat-events.js
// produces; owns no event-vocabulary knowledge itself.

// renderMarkdown converts markdown to sanitized HTML. Falls back to escaped
// plain text if the vendored markdown/sanitizer libs are not loaded (e.g. a
// unit test running without the real page scripts).
function renderMarkdown(text) {
    const md = typeof window !== 'undefined' && window.marked;
    const purify = typeof window !== 'undefined' && window.DOMPurify;
    if (!md || !purify) return escapeText(text);
    return purify.sanitize(md.parse(text || ''));
}

function escapeText(str) {
    return String(str || '')
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// Element lookup uses a plain cache keyed off the list/message element
// itself, not a CSS attribute-selector query — a query string built from an
// event id would need selector-escaping to be robust, and repeated attribute
// lookups don't scale as the message list grows. data-* attributes are still
// set for real-DOM inspection/debugging, just never read back.

// findOrCreateMessageEl returns the message bubble element for messageId,
// creating and appending it (with a role class) if it doesn't exist yet.
function findOrCreateMessageEl(listEl, messageId, role) {
    const cache = listEl._chatMessageEls || (listEl._chatMessageEls = {});
    if (cache[messageId]) return cache[messageId];
    // One turn spans several engine messages (around tool calls); consecutive
    // assistant messages share one visual box. Any other element appended
    // between them (user bubble, system notice) breaks the group.
    const last = listEl.children[listEl.children.length - 1];
    if (role === 'assistant' && last && last.classList.contains('chat-msg-assistant')) {
        cache[messageId] = last;
        return last;
    }
    const el = document.createElement('div');
    el.className = 'chat-msg chat-msg-' + role;
    el.setAttribute('data-message-id', messageId);
    listEl.appendChild(el);
    cache[messageId] = el;
    return el;
}

// findOrCreateBlockEl returns blockKey's element within msgEl. Block indices
// are scoped to their engine message, and merged turns put several messages
// in one msgEl, so the key is messageId-qualified.
function findOrCreateBlockEl(msgEl, blockKey, type) {
    const cache = msgEl._chatBlockEls || (msgEl._chatBlockEls = {});
    if (cache[blockKey]) return cache[blockKey];
    const el = document.createElement('div');
    el.className = 'chat-block chat-block-' + type;
    el.setAttribute('data-block-index', String(blockKey));
    msgEl.appendChild(el);
    cache[blockKey] = el;
    return el;
}

// renderTextBlock renders a text block's current (possibly partial) content
// as markdown — re-parsed on every append, which is simple and correct; for
// pathologically long single messages a future pass could diff instead.
function renderTextBlock(el, block) {
    el.innerHTML = renderMarkdown(block.text);
}

// renderThinkingBlock renders a collapsed-by-default thinking block with an
// expand affordance.
function renderThinkingBlock(el, block) {
    el.innerHTML = '';
    // No text yet (still streaming) or none ever (redacted / transcript
    // shell): render nothing rather than a toggle over an empty body.
    if (!block.text) return;
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'chat-thinking-toggle';
    toggle.textContent = (block.collapsed ? '▸' : '▾') + ' thinking';
    toggle.onclick = () => {
        block.collapsed = !block.collapsed;
        renderThinkingBlock(el, block);
    };
    el.appendChild(toggle);
    if (!block.collapsed) {
        const body = document.createElement('div');
        body.className = 'chat-thinking-body';
        body.textContent = block.text;
        el.appendChild(body);
    }
}

// diffLines renders a crude unified-style diff for Edit/Write tool inputs:
// old_string/new_string (Edit) or file_path/content (Write). Good enough for
// a v1 collapsible row — a full line-level diff algorithm is future work.
function renderEditDiff(input) {
    const wrap = document.createElement('div');
    wrap.className = 'chat-diff';
    if (input && typeof input.old_string === 'string') {
        const del = document.createElement('pre');
        del.className = 'chat-diff-del';
        del.textContent = input.old_string;
        const add = document.createElement('pre');
        add.className = 'chat-diff-add';
        add.textContent = input.new_string || '';
        wrap.appendChild(del);
        wrap.appendChild(add);
    } else if (input && typeof input.content === 'string') {
        const add = document.createElement('pre');
        add.className = 'chat-diff-add';
        add.textContent = input.content;
        wrap.appendChild(add);
    }
    return wrap;
}

// renderToolBlock renders one collapsible tool-call row: Edit/Write as a
// diff, Bash as command + output excerpt, everything else as name + raw
// input/output.
function renderToolBlock(el, block) {
    el.innerHTML = '';
    const header = document.createElement('button');
    header.type = 'button';
    header.className = 'chat-tool-toggle';
    // Open-state lives on the block (not the DOM) so a re-render on
    // tool-result arrival doesn't snap an opened body shut.
    const label = () => (block.open ? '▾ ' : '▸ ') + (block.toolName || 'tool') + (block.done ? '' : '…');
    header.textContent = label();
    const body = document.createElement('div');
    body.className = 'chat-tool-body' + (block.open ? '' : ' hidden');
    header.onclick = () => {
        block.open = !block.open;
        body.classList.toggle('hidden', !block.open);
        header.textContent = label();
    };
    el.appendChild(header);
    el.appendChild(body);

    const input = block.input;
    if (block.toolName === 'Edit' || block.toolName === 'Write') {
        body.appendChild(renderEditDiff(input));
    } else if (block.toolName === 'Bash') {
        const cmd = document.createElement('pre');
        cmd.className = 'chat-tool-cmd';
        cmd.textContent = (input && input.command) || '';
        body.appendChild(cmd);
    } else {
        const raw = document.createElement('pre');
        raw.className = 'chat-tool-input';
        raw.textContent = input ? JSON.stringify(input, null, 2) : (block.inputRaw || '');
        body.appendChild(raw);
    }
    if (block.resultText) {
        const out = document.createElement('pre');
        out.className = 'chat-tool-output';
        out.textContent = block.resultText;
        body.appendChild(out);
    }
}

function renderBlock(msgEl, messageId, blockIndex, block) {
    const el = findOrCreateBlockEl(msgEl, messageId + ':' + blockIndex, block.type);
    if (block.type === 'thinking') renderThinkingBlock(el, block);
    else if (block.type === 'tool') renderToolBlock(el, block);
    else renderTextBlock(el, block);
}

// appendSystemNotice renders a plain system line (e.g. after /clear).
export function appendSystemNotice(listEl, text) {
    const el = document.createElement('div');
    el.className = 'chat-msg chat-msg-system';
    el.textContent = text;
    listEl.appendChild(el);
    scrollIfFollowing(listEl);
}

// appendUserMessage renders the user's own message optimistically on send —
// the server's "user" event echo is intentionally NOT re-rendered (see
// chat-events.js applyUserEvent) to avoid a duplicate bubble.
export function appendUserMessage(listEl, text) {
    const el = document.createElement('div');
    el.className = 'chat-msg chat-msg-user';
    el.textContent = text;
    listEl.appendChild(el);
    // Sending re-engages the follow no matter where the user had scrolled.
    ensureFollowTracking(listEl);
    listEl._chatFollow = true;
    listEl.scrollTop = listEl.scrollHeight;
}

// Sticky follow: the list auto-scrolls while the user sits at the bottom and
// stops when they scroll up to read. State comes from scroll events — never
// from measuring after an append (a single chunk taller than the threshold
// would land the measurement "far from bottom" and break the follow for good).
function ensureFollowTracking(listEl) {
    if (listEl._chatFollowBound) return;
    listEl._chatFollowBound = true;
    if (listEl._chatFollow === undefined) listEl._chatFollow = true;
    listEl.addEventListener('scroll', () => {
        listEl._chatFollow = listEl.scrollHeight - listEl.scrollTop - listEl.clientHeight < 120;
    });
}

function scrollIfFollowing(listEl) {
    ensureFollowTracking(listEl);
    if (listEl._chatFollow) listEl.scrollTop = listEl.scrollHeight;
}

// applyPatches renders one batch of chat-events.js patches into listEl.
export function applyPatches(listEl, patches, callbacks = {}) {
    for (const p of patches) {
        switch (p.kind) {
            case 'new-message':
                findOrCreateMessageEl(listEl, p.messageId, p.role);
                break;
            case 'new-block':
            case 'append-text':
            case 'tool-input-progress':
            case 'finalize-block':
            case 'tool-result': {
                const msgEl = findOrCreateMessageEl(listEl, p.messageId, 'assistant');
                renderBlock(msgEl, p.messageId, p.blockIndex, p.block);
                break;
            }
            case 'finalize-message':
                break;
            case 'system-notice':
                appendSystemNotice(listEl, p.text);
                break;
            case 'header':
                callbacks.onHeader?.(p.header);
                break;
            case 'usage':
                callbacks.onUsage?.(p.usage);
                break;
            default:
                break;
        }
    }
    if (patches.length) scrollIfFollowing(listEl);
}
