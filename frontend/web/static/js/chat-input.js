// Chat input bar: send, stop-while-running (the send button becomes a stop
// button once a turn is in flight, calling onStop to interrupt), /clear as a
// plain typed line (no dedicated button), and image attach via the existing
// upload endpoint + file-path reference (never inline image bytes).

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
// imagePath) fires on submit; onStop() fires when the button is clicked while
// a turn is running (see setRunning in the return value).
export function createInputBar({ onSend, onStop, terminalId }) {
    const wrap = document.createElement('div');
    wrap.className = 'chat-input-bar';

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

    wrap.appendChild(attachBtn);
    wrap.appendChild(textarea);
    wrap.appendChild(fileInput);
    wrap.appendChild(sendBtn);

    let pendingFilePath = null;
    let running = false;

    function clearAttachment() {
        pendingFilePath = null;
        attachBtn.classList.remove('chat-input-attach-pending');
    }

    function doSend() {
        const text = textarea.value.trim();
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
    fileInput.addEventListener('change', async () => {
        const file = fileInput.files && fileInput.files[0];
        fileInput.value = '';
        if (!file) return;
        try {
            pendingFilePath = await uploadFile(terminalId, file);
            attachBtn.classList.add('chat-input-attach-pending');
        } catch (e) {
            clearAttachment();
        }
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
