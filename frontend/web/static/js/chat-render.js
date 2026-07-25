// DOM rendering for the chat message list: markdown, streaming text, tool
// step rows (diff for Edit/Write, command+output for Bash), collapsed
// thinking blocks, and system notices. Consumes the patches chat-events.js
// produces; owns no event-vocabulary knowledge itself. Scroll behavior is
// deliberately NOT here — chat-scroll.js owns it (observation-driven).

import { copyToClipboard } from './ui-utils.js';
import { stripQuickReplyMarker } from './chat-events.js';

// makeCopyButton builds an icon copy button (currentColor-masked glyph, not a
// text label) whose click copies getText() and briefly swaps the glyph to a
// checkmark. getText is read at click time so it reflects the latest content.
function makeCopyButton(className, getText) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = className;
    btn.title = 'Copy';
    btn.setAttribute('aria-label', 'Copy');
    const icon = document.createElement('span');
    icon.className = 'chat-copy-icon';
    btn.appendChild(icon);
    btn.addEventListener('click', (e) => {
        e.stopPropagation(); // don't toggle a tool row / trip other handlers
        Promise.resolve(copyToClipboard(getText())).then((ok) => {
            btn.classList.toggle('copied', ok);
            btn.title = ok ? 'Copied' : 'Copy failed';
            setTimeout(() => { btn.classList.remove('copied'); btn.title = 'Copy'; }, 1200);
        });
    });
    return btn;
}

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

// ---- Syntax highlighting (vendored highlight.js; optional at runtime) ----

// langFromPath maps a file extension to a highlight.js language id, or ''.
// Exported for tests.
export function langFromPath(path) {
    const ext = String(path || '').match(/\.([a-z0-9]+)$/i)?.[1]?.toLowerCase() || '';
    const map = {
        js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
        ts: 'typescript', tsx: 'typescript',
        py: 'python', rb: 'ruby', go: 'go', rs: 'rust', java: 'java', kt: 'kotlin',
        c: 'c', h: 'c', cpp: 'cpp', cc: 'cpp', hpp: 'cpp', cs: 'csharp',
        sh: 'bash', bash: 'bash', zsh: 'bash',
        json: 'json', yaml: 'yaml', yml: 'yaml', toml: 'ini', ini: 'ini',
        html: 'xml', xml: 'xml', css: 'css', scss: 'scss', sql: 'sql',
        md: 'markdown', php: 'php', swift: 'swift', dart: 'dart',
    };
    return map[ext] || '';
}

// highlightCode highlights one pre/code-ish element's text content in place.
// lang '' = highlight.js auto-detect. No-op without the vendored lib.
function highlightCode(el, lang) {
    const hljs = typeof window !== 'undefined' && window.hljs;
    if (!hljs || el._highlighted) return;
    try {
        const text = el.textContent;
        const res = lang && hljs.getLanguage(lang)
            ? hljs.highlight(text, { language: lang })
            : hljs.highlightAuto(text);
        el.innerHTML = res.value; // hljs output is escaped span markup
        el.classList.add('hljs');
        el._highlighted = true;
    } catch (e) { /* leave plain text */ }
}

// highlightFences highlights every fenced code block under a rendered
// markdown container (language hint from the fence's language-* class).
function highlightFences(container) {
    if (typeof window === 'undefined' || !window.hljs || !container.querySelectorAll) return;
    for (const code of container.querySelectorAll('pre code')) {
        const lang = [...code.classList].find(c => c.startsWith('language-'))?.slice(9) || '';
        highlightCode(code, lang);
    }
}

// decorateCodeBlocks adds a copy button to each fenced code block. The code
// text is captured up front, so the button (a sibling of the pre inside a
// positioned wrapper — never inside the pre) can't pollute what gets copied.
function decorateCodeBlocks(container) {
    if (!container.querySelectorAll) return; // real DOM only
    for (const pre of container.querySelectorAll('pre')) {
        if (pre._copyWrapped || !pre.parentNode) continue;
        // Diff panes are a paired old/new layout, not a standalone copy target.
        if (pre.parentNode.classList && pre.parentNode.classList.contains('chat-diff')) continue;
        pre._copyWrapped = true;
        const code = pre.textContent;
        const wrap = document.createElement('div');
        wrap.className = 'chat-code';
        pre.parentNode.insertBefore(wrap, pre);
        wrap.appendChild(pre);
        wrap.appendChild(makeCopyButton('chat-code-copy', () => code));
    }
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
    if (role === 'assistant') {
        // Mark this as the active turn: its avatar pulses while the turn runs
        // (CSS .is-turn-running .is-active-turn), giving continuous "working"
        // feedback through every phase — wait, thinking, tools, streaming — so
        // it never looks hung, regardless of first-block type or speed.
        if (listEl._activeAssistantEl) listEl._activeAssistantEl.classList.remove('is-active-turn');
        el.classList.add('is-active-turn');
        listEl._activeAssistantEl = el;
        // Avatar + content column. The ✱ avatar is a flex item aligned to the
        // content's first-line baseline (CSS align-items:baseline), so it rides
        // the text at any font-size/line-height/first-block type — no
        // hand-tuned offset to drift. Blocks, timestamp, and copy button live
        // in the column (el._body); the avatar is the left gutter.
        const avatar = document.createElement('span');
        avatar.className = 'chat-msg-avatar';
        avatar.setAttribute('aria-hidden', 'true');
        avatar.textContent = '✱';
        el.appendChild(avatar);
        const body = document.createElement('div');
        body.className = 'chat-assist-body';
        el.appendChild(body);
        el._body = body;
        // Turn-copy: the prose only. Reads the markdown source stashed on each
        // text block (_src), so thinking and tool blocks are structurally
        // excluded rather than filtered by class-sniffing. Hidden until the
        // turn actually has prose (a tool-only turn has nothing to copy).
        const copyBtn = makeCopyButton('chat-turn-copy hidden', () =>
            [...body.children]
                .filter((c) => c.classList?.contains('chat-block-text'))
                .map((c) => c._src || '')
                .filter(Boolean)
                .join('\n\n'));
        el._turnCopyBtn = copyBtn;
        body.appendChild(copyBtn);
    }
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
    (msgEl._body || msgEl).appendChild(el);
    cache[blockKey] = el;
    return el;
}

// stableMarkdownBoundary returns the end index of the last blank line that
// sits OUTSIDE any code fence. Text before it is "stable" — appending more
// text can no longer change how it parses (paragraph-level granularity) — so
// streaming re-renders only the open tail. Exported for tests.
export function stableMarkdownBoundary(text) {
    const src = String(text || '');
    let inFence = false;
    let boundary = 0;
    let pos = 0;
    for (const line of src.split('\n')) {
        if (/^(```|~~~)/.test(line.trimStart())) inFence = !inFence;
        pos += line.length + 1;
        if (!inFence && line.trim() === '') boundary = Math.min(pos, src.length);
    }
    return boundary;
}

// renderTextBlock renders a text block's current (possibly partial) content.
// Streaming is incremental: stable paragraphs are parsed once and their HTML
// cached cumulatively; each delta re-parses only the open tail — O(n) total
// instead of re-parsing the whole text per delta. Segment-isolated parsing
// can differ from a whole-text parse for constructs spanning blank lines
// (loose lists), so finalize does one authoritative full parse.
function renderTextBlock(el, block) {
    // Strip the quick-reply marker (complete, or still-streaming trailing open)
    // so it never renders and never lands in the copy source.
    const text = stripQuickReplyMarker(block.text || '').replace(/\[\[reply:[^\]]*$/, '');
    el._src = text; // markdown source, for turn-copy
    if (block.done) {
        el._md = null;
        el.innerHTML = renderMarkdown(text);
        // Highlight only once the block stops streaming — re-highlighting
        // every delta would burn CPU for no visible gain.
        highlightFences(el);
        decorateCodeBlocks(el);
        return;
    }
    const boundary = stableMarkdownBoundary(text);
    const stable = text.slice(0, boundary);
    if (!el._md || !stable.startsWith(el._md.src)) {
        el._md = { src: '', html: '' }; // fresh block (streams are append-only)
    }
    if (stable.length > el._md.src.length) {
        el._md.html += renderMarkdown(stable.slice(el._md.src.length));
        el._md.src = stable;
    }
    el.innerHTML = el._md.html + renderMarkdown(text.slice(boundary));
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

// toolPreview returns a short single-line summary of a tool call's key input
// (command, file path, pattern, …) for the collapsed row, so a stack of tool
// calls is scannable at a glance instead of a wall of identical "BASH" rows.
function toolPreview(block) {
    const inp = block.input;
    if (!inp || typeof inp !== 'object') return '';
    const first = (s) => String(s == null ? '' : s).split('\n')[0].trim();
    switch (block.toolName) {
        case 'Bash': return first(inp.command);
        case 'Edit':
        case 'Write':
        case 'Read': return first(inp.file_path);
        case 'NotebookEdit': return first(inp.notebook_path);
        case 'Grep': return first(inp.pattern) + (inp.path ? '  ' + first(inp.path) : '');
        case 'Glob': return first(inp.pattern);
        case 'Task': return first(inp.description || inp.subagent_type);
        case 'WebFetch': return first(inp.url);
        case 'WebSearch': return first(inp.query);
        case 'Skill': return first(inp.skill || inp.command);
        default: {
            // Fallback: the first non-empty string field.
            const v = Object.values(inp).find((x) => typeof x === 'string' && x.trim());
            return first(v);
        }
    }
}

// renderToolBlock renders one collapsible tool-call row: a header (name + a
// one-line input preview) over a body — Edit/Write as a diff, Bash as command
// + output excerpt, everything else as name + raw input/output.
function renderToolBlock(el, block) {
    el.innerHTML = '';
    const input = block.input;
    const fileLang = langFromPath(input?.file_path);
    // Command-like tools show their key input as an always-visible, boxed,
    // syntax-highlighted snippet (the "preview in a box") rather than a one-line
    // header summary; file/other tools keep the compact header preview.
    const boxed = block.toolName === 'Bash' || block.toolName === 'Grep' || block.toolName === 'Glob';

    const header = document.createElement('button');
    header.type = 'button';
    header.className = 'chat-tool-toggle';
    const nameEl = document.createElement('span');
    nameEl.className = 'chat-tool-name';
    const previewEl = document.createElement('span');
    previewEl.className = 'chat-tool-preview';
    header.appendChild(nameEl);
    header.appendChild(previewEl);
    // Open-state lives on the block (not the DOM) so a re-render on
    // tool-result arrival doesn't snap an opened body shut.
    const renderHeader = () => {
        nameEl.textContent = (block.open ? '▾ ' : '▸ ') + (block.toolName || 'tool') + (block.done ? '' : '…');
        const pv = boxed ? '' : toolPreview(block); // boxed tools show input in the box below
        previewEl.textContent = pv;
        previewEl.classList.toggle('hidden', !pv);
    };
    renderHeader();
    el.appendChild(header);

    // Always-visible boxed preview: the command (Bash) or pattern (Grep/Glob),
    // highlighted, so a stack of tool calls reads at a glance.
    if (boxed) {
        const cmd = document.createElement('pre');
        cmd.className = 'chat-tool-cmd';
        cmd.textContent = block.toolName === 'Bash'
            ? ((input && input.command) || '')
            : (input && input.pattern ? input.pattern + (input.path ? '   ' + input.path : '') : '');
        if (cmd.textContent) {
            el.appendChild(cmd);
            if (block.done && block.toolName === 'Bash') highlightCode(cmd, 'bash');
        }
    }

    const body = document.createElement('div');
    body.className = 'chat-tool-body' + (block.open ? '' : ' hidden');
    header.onclick = () => {
        block.open = !block.open;
        body.classList.toggle('hidden', !block.open);
        renderHeader();
    };
    el.appendChild(body);

    // Collapsible detail: diff for Edit/Write, raw input for non-boxed tools,
    // and the result output for any tool.
    if (block.toolName === 'Edit' || block.toolName === 'Write') {
        const diff = renderEditDiff(input);
        body.appendChild(diff);
        if (fileLang) for (const pane of diff.children) highlightCode(pane, fileLang);
    } else if (!boxed) {
        const raw = document.createElement('pre');
        raw.className = 'chat-tool-input';
        raw.textContent = input ? JSON.stringify(input, null, 2) : (block.inputRaw || '');
        body.appendChild(raw);
        if (block.done && input) highlightCode(raw, 'json');
    }
    if (block.resultText) {
        const out = document.createElement('pre');
        out.className = 'chat-tool-output';
        out.textContent = block.resultText;
        body.appendChild(out);
        // Read excerpts get the file's language; other outputs stay plain
        // (auto-detect on arbitrary tool output guesses wrong too often).
        if (block.toolName === 'Read' && fileLang) highlightCode(out, fileLang);
    }
    // Same copy affordance as markdown code fences. Decorate the whole block —
    // the boxed command lives outside the collapsible body.
    decorateCodeBlocks(el);
}

function renderBlock(msgEl, messageId, blockIndex, block) {
    const el = findOrCreateBlockEl(msgEl, messageId + ':' + blockIndex, block.type);
    if (block.type === 'thinking') renderThinkingBlock(el, block);
    else if (block.type === 'tool') renderToolBlock(el, block);
    else {
        renderTextBlock(el, block);
        if (block.text) msgEl._turnCopyBtn?.classList.remove('hidden'); // turn now has prose
    }
}

// resetView empties a render container AND its element caches — clearing
// innerHTML alone would leave the message cache pointing at detached nodes,
// so a rebuild would render into elements no longer in the DOM.
export function resetView(listEl) {
    listEl.innerHTML = '';
    listEl._chatMessageEls = null;
    listEl._chatPending = null;
    listEl._chatConnNotice = null;
    listEl._activeAssistantEl = null;
}

// showPending/clearPending: the "thinking…" row between a user's send and the
// first streamed content. Shown explicitly on live sends (local echo via
// chat.js, mirrored co-viewer sends via the user-message patch) — never on
// history replay, where a conversation that died on a user turn would
// otherwise dangle a forever-pending row.
export function showPending(listEl) {
    if (listEl._chatPending) return;
    const el = document.createElement('div');
    el.className = 'chat-pending';
    const avatar = document.createElement('span');
    avatar.className = 'chat-msg-avatar';
    avatar.setAttribute('aria-hidden', 'true');
    avatar.textContent = '✱';
    const label = document.createElement('span');
    label.textContent = 'thinking…';
    el.appendChild(avatar);
    el.appendChild(label);
    listEl.appendChild(el);
    listEl._chatPending = el;
}

export function clearPending(listEl) {
    if (!listEl._chatPending) return;
    listEl._chatPending.remove();
    listEl._chatPending = null;
}

// appendInterruptMarker renders a small muted "Stopped" line indented to sit
// under the turn it cut short (not a bubble, not a full-width divider).
export function appendInterruptMarker(listEl) {
    const el = document.createElement('div');
    el.className = 'chat-interrupted';
    el.textContent = '■ Stopped'; // machined square, not the ⏹ emoji
    listEl.appendChild(el);
}

// attachmentIcon builds the inline attachment glyph — a CSS-masked paperclip
// in currentColor (Futurism: icons are currentColor, never color emoji).
export function attachmentIcon() {
    const icon = document.createElement('span');
    icon.className = 'chat-attach-icon';
    icon.setAttribute('aria-label', 'attachment');
    return icon;
}

// renderQuickReplies appends a row of tappable option chips; a tap sends that
// option's text as the next message via onQuickReply. Only one row lives at a
// time — a new row (or a user send) clears the previous, since old options
// are stale once the conversation moves on.
export function renderQuickReplies(listEl, options, onQuickReply) {
    clearQuickReplies(listEl);
    if (!options || !options.length) return;
    const row = document.createElement('div');
    row.className = 'chat-quick-replies';
    for (const opt of options) {
        const chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'chat-quick-reply';
        chip.textContent = opt;
        chip.addEventListener('click', () => { clearQuickReplies(listEl); onQuickReply?.(opt); });
        row.appendChild(chip);
    }
    listEl.appendChild(row);
}

function clearQuickReplies(listEl) {
    for (const row of [...listEl.children]) {
        if (row.classList && row.classList.contains('chat-quick-replies')) row.remove();
    }
}

// timeEl builds a subtle timestamp element. ts may be an ISO string, epoch
// ms, or null/undefined (→ now, for live messages). Full date on hover.
function timeEl(ts) {
    const el = document.createElement('span');
    el.className = 'chat-time';
    const d = ts != null ? new Date(ts) : new Date();
    if (isNaN(d.getTime())) return el;
    el.textContent = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    el.title = d.toLocaleString();
    return el;
}

// appendSystemNotice renders a plain system line (e.g. after /clear).
export function appendSystemNotice(listEl, text) {
    const el = document.createElement('div');
    el.className = 'chat-msg chat-msg-system';
    el.textContent = text;
    listEl.appendChild(el);
}

// setConnectionNotice shows a single connection-status line (reconnecting /
// lost) that updates in place, so repeated reconnect attempts refresh one
// notice instead of stacking a new box each time. clearConnectionNotice
// removes it (on reconnect, or before a terminal [Session ended]).
export function setConnectionNotice(listEl, text) {
    let el = listEl._chatConnNotice;
    if (!el) {
        el = document.createElement('div');
        el.className = 'chat-msg chat-msg-system';
        listEl._chatConnNotice = el;
        listEl.appendChild(el);
    }
    el.textContent = text;
}

export function clearConnectionNotice(listEl) {
    if (!listEl._chatConnNotice) return;
    listEl._chatConnNotice.remove();
    listEl._chatConnNotice = null;
}

// appendUserMessage renders the user's own message optimistically on send —
// the server's "user" event echo is intentionally NOT re-rendered (see
// chat-events.js applyUserEvent) to avoid a duplicate bubble.
export function appendUserMessage(listEl, text, hasAttachment, ts, opts) {
    clearQuickReplies(listEl); // any pending chips are answered once the user speaks
    // A new user turn ends the previous assistant turn — drop its active mark so
    // its avatar stops pulsing (the next turn's message takes over).
    if (listEl._activeAssistantEl) {
        listEl._activeAssistantEl.classList.remove('is-active-turn');
        listEl._activeAssistantEl = null;
    }
    const el = document.createElement('div');
    el.className = 'chat-msg chat-msg-user';
    if (text) {
        const span = document.createElement('span');
        span.className = 'chat-msg-body';
        span.textContent = text; // textContent keeps user input un-parsed
        el.appendChild(span);
    }
    if (hasAttachment) el.appendChild(attachmentIcon());
    el.appendChild(timeEl(ts));
    // Unsent: the socket wasn't open, so the message was echoed but not
    // delivered. Mark it and offer a retry (which removes this bubble and
    // re-sends) — never leave a bubble that looks sent but wasn't.
    if (opts && opts.unsent) {
        el.classList.add('chat-msg-unsent');
        const retry = document.createElement('button');
        retry.type = 'button';
        retry.className = 'chat-retry';
        retry.textContent = 'Not sent — retry';
        retry.addEventListener('click', () => { el.remove(); opts.onRetry?.(); });
        el.appendChild(retry);
    }
    listEl.appendChild(el);
    return el;
}

// applyPatches renders one batch of chat-events.js patches into listEl.
export function applyPatches(listEl, patches, callbacks = {}) {
    for (const p of patches) {
        switch (p.kind) {
            case 'new-message': {
                const msgEl = findOrCreateMessageEl(listEl, p.messageId, p.role);
                // First engine message of a (possibly merged) turn stamps the
                // time once; later merged messages reuse the box, don't restamp.
                if (!msgEl._timeSet) { (msgEl._body || msgEl).appendChild(timeEl(p.ts)); msgEl._timeSet = true; }
                break;
            }
            case 'new-block':
            case 'append-text':
            case 'tool-input-progress':
            case 'finalize-block':
            case 'tool-result': {
                clearPending(listEl);
                const msgEl = findOrCreateMessageEl(listEl, p.messageId, 'assistant');
                renderBlock(msgEl, p.messageId, p.blockIndex, p.block);
                break;
            }
            case 'finalize-message':
                break;
            case 'system-notice':
                clearPending(listEl);
                appendSystemNotice(listEl, p.text);
                break;
            case 'user-message':
                appendUserMessage(listEl, p.text, p.attachment, p.ts);
                showPending(listEl); // a co-viewer sent; their turn is now running
                break;
            case 'quick-replies':
                renderQuickReplies(listEl, p.options, callbacks.onQuickReply);
                break;
            case 'interrupt':
                clearPending(listEl);
                appendInterruptMarker(listEl);
                break;
            case 'header':
                callbacks.onHeader?.(p.header);
                break;
            default:
                break;
        }
    }
}
