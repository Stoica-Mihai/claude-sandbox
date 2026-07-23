// Chat input bar: send, stop-while-running (the send button becomes a stop
// button once a turn is in flight, calling onStop to interrupt), /clear as a
// plain typed line (no dedicated button), and file attach via the existing
// upload endpoint + file-path reference (never inline file bytes).

import { sessionUploadPath } from './routes.js';

// uploadFile posts any file to the session's upload endpoint and returns the
// saved path the model can Read (images, PDFs, docs, source — engine's call).
export async function uploadFile(terminalId, file) {
    const form = new FormData();
    form.append('file', file);
    const res = await fetch(sessionUploadPath(terminalId), { method: 'POST', body: form });
    if (!res.ok) throw new Error('upload failed: ' + res.status);
    const data = await res.json();
    return data.path;
}

// createInputBar builds the input bar DOM and wires send/attach. onSend(text,
// filePath) fires on submit; onStop() fires when the button is clicked while
// a turn is running (see setRunning in the return value).
export function createInputBar({ onSend, onStop, terminalId }) {
    const wrap = document.createElement('div');
    wrap.className = 'chat-input-bar';

    // Attachment chip (own line, above the controls via flex-wrap): shows the
    // selected file and its upload state so the attachment is visible before
    // send — a silent button tint was easy to miss, especially on mobile.
    const chip = document.createElement('div');
    chip.className = 'chat-input-chip hidden';

    const textarea = document.createElement('textarea');
    textarea.className = 'chat-input-text';
    // Long placeholder wraps and clips at phone width — keep the /clear hint
    // desktop-only (title tooltip carries it everywhere).
    const compact = typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(max-width:640px)').matches;
    textarea.placeholder = compact ? 'Message…' : 'Message… (/clear resets context, same folder)';
    textarea.title = '/clear resets context, same folder';
    textarea.rows = 1;

    const fileInput = document.createElement('input');
    fileInput.type = 'file';
    fileInput.className = 'chat-input-file hidden'; // no accept filter — any file

    const attachBtn = document.createElement('button');
    attachBtn.type = 'button';
    attachBtn.className = 'chat-input-attach';
    attachBtn.title = 'Attach file';
    attachBtn.setAttribute('aria-label', 'Attach file');
    attachBtn.textContent = '+';

    const sendBtn = document.createElement('button');
    sendBtn.type = 'button';
    sendBtn.className = 'chat-input-send';
    sendBtn.textContent = 'Send';

    wrap.appendChild(chip);
    wrap.appendChild(attachBtn);
    wrap.appendChild(textarea);
    wrap.appendChild(fileInput);
    wrap.appendChild(sendBtn);

    let pendingFilePath = null;
    let pendingUpload = null; // in-flight upload promise, or null
    let running = false;

    // renderChip reflects the attachment state; status: uploading|ready|failed
    // or null to hide. A ready chip carries a remove control.
    function renderChip(name, status) {
        chip.textContent = '';
        if (!name) { chip.classList.add('hidden'); return; }
        chip.classList.remove('hidden');
        const label = document.createElement('span');
        label.className = 'chat-chip-name';
        const suffix = status === 'uploading' ? ' · uploading…' : status === 'failed' ? ' · upload failed' : '';
        label.textContent = '📎 ' + name + suffix;
        chip.appendChild(label);
        if (status === 'ready') {
            const rm = document.createElement('button');
            rm.type = 'button';
            rm.className = 'chat-chip-remove';
            rm.title = 'Remove attachment';
            rm.setAttribute('aria-label', 'Remove attachment');
            rm.textContent = '✕';
            rm.addEventListener('click', clearAttachment);
            chip.appendChild(rm);
        }
    }

    function clearAttachment() {
        pendingFilePath = null;
        pendingUpload = null;
        renderChip(null);
    }

    async function doSend() {
        const text = textarea.value.trim();
        // Wait out an in-flight upload so a fast send can't drop the file.
        if (pendingUpload) { try { await pendingUpload; } catch (e) { /* failure already shown on the chip */ } }
        if (!text && !pendingFilePath) return;
        onSend(text, pendingFilePath);
        textarea.value = '';
        clearAttachment();
    }

    // setRunning toggles the primary button between Send and Stop. While
    // running, the button interrupts the turn (onStop) and Enter does not send.
    function setRunning(on) {
        running = on;
        sendBtn.textContent = on ? 'Stop' : 'Send';
        sendBtn.classList.toggle('chat-input-stop', on);
    }

    attachBtn.addEventListener('click', () => fileInput.click());
    fileInput.addEventListener('change', () => {
        const file = fileInput.files && fileInput.files[0];
        fileInput.value = '';
        if (!file) return;
        pendingFilePath = null;
        renderChip(file.name, 'uploading');
        pendingUpload = uploadFile(terminalId, file)
            .then((path) => { pendingFilePath = path; renderChip(file.name, 'ready'); })
            .catch(() => { renderChip(file.name, 'failed'); })
            .finally(() => { pendingUpload = null; });
    });

    sendBtn.addEventListener('click', () => {
        if (running) onStop?.();
        else doSend();
    });
    textarea.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            if (!running) doSend();
        }
    });

    return { el: wrap, focus: () => textarea.focus(), setRunning };
}
