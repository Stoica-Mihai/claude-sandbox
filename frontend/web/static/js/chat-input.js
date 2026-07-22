// Chat input bar: send, queue-while-running (ordering is guaranteed
// server-side by the single-actor chat session — see chat-session-host — so
// the client just sends in submission order), /clear as a plain typed line
// (no dedicated button), and image attach via the existing upload endpoint +
// file-path reference (never inline image bytes).

import { sessionUploadPath } from './routes.js';

// uploadImageFile posts an image file to the session's upload endpoint and
// returns the saved file path the model can Read.
export async function uploadImageFile(terminalId, file) {
    const form = new FormData();
    form.append('image', file);
    const res = await fetch(sessionUploadPath(terminalId), { method: 'POST', body: form });
    if (!res.ok) throw new Error('upload failed: ' + res.status);
    const data = await res.json();
    return data.path;
}

// createInputBar builds the input bar DOM and wires send/attach. onSend(text,
// imagePath) is called with the user's submitted text and an optional
// uploaded-image path.
export function createInputBar({ onSend, terminalId }) {
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
    fileInput.accept = 'image/*';
    fileInput.className = 'chat-input-file hidden';

    const attachBtn = document.createElement('button');
    attachBtn.type = 'button';
    attachBtn.className = 'chat-input-attach';
    attachBtn.title = 'Attach image';
    attachBtn.setAttribute('aria-label', 'Attach image');
    attachBtn.textContent = '+';

    const sendBtn = document.createElement('button');
    sendBtn.type = 'button';
    sendBtn.className = 'chat-input-send';
    sendBtn.textContent = 'Send';

    wrap.appendChild(attachBtn);
    wrap.appendChild(textarea);
    wrap.appendChild(fileInput);
    wrap.appendChild(sendBtn);

    let pendingImagePath = null;

    function clearAttachment() {
        pendingImagePath = null;
        attachBtn.classList.remove('chat-input-attach-pending');
    }

    function doSend() {
        const text = textarea.value.trim();
        if (!text && !pendingImagePath) return;
        onSend(text, pendingImagePath);
        textarea.value = '';
        clearAttachment();
    }

    attachBtn.addEventListener('click', () => fileInput.click());
    fileInput.addEventListener('change', async () => {
        const file = fileInput.files && fileInput.files[0];
        fileInput.value = '';
        if (!file) return;
        try {
            pendingImagePath = await uploadImageFile(terminalId, file);
            attachBtn.classList.add('chat-input-attach-pending');
        } catch (e) {
            clearAttachment();
        }
    });

    sendBtn.addEventListener('click', doSend);
    textarea.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            doSend();
        }
    });

    return { el: wrap, focus: () => textarea.focus() };
}
