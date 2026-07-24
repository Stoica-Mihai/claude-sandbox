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
    if (!res.ok) {
        let detail = '';
        try { detail = (await res.json()).error || ''; } catch (e) { /* non-JSON error body */ }
        throw new Error(detail || ('HTTP ' + res.status));
    }
    const data = await res.json();
    return data.path;
}

// InputBar builds the input bar DOM and wires send/attach. onSend(text,
// filePath) fires on submit; onStop() fires when the button is clicked while a
// turn is running. `focus`/`setRunning` are bound arrow fields so a caller can
// hold and invoke them detached from the instance (ChatView does).
export class InputBar {
    constructor({ onSend, onStop, terminalId }) {
        this.onSend = onSend;
        this.onStop = onStop;
        this.terminalId = terminalId;
        this.pendingFilePath = null;
        this.pendingUpload = null; // in-flight upload promise, or null
        this.running = false;

        const wrap = this.el = document.createElement('div');
        wrap.className = 'chat-input-bar';

        // Attachment chip (own line, above the controls via flex-wrap): shows
        // the selected file and its upload state so the attachment is visible
        // before send — a silent button tint was easy to miss, especially on
        // mobile.
        const chip = this.chip = document.createElement('div');
        chip.className = 'chat-input-chip hidden';

        const textarea = this.textarea = document.createElement('textarea');
        textarea.className = 'chat-input-text';
        // Long placeholder wraps and clips at phone width — keep the /clear hint
        // desktop-only (title tooltip carries it everywhere).
        const compact = typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(max-width:640px)').matches;
        textarea.placeholder = compact ? 'Message…' : 'Message… (/clear resets context, same folder)';
        textarea.title = '/clear resets context, same folder';
        textarea.rows = 1;

        const fileInput = this.fileInput = document.createElement('input');
        fileInput.type = 'file';
        fileInput.className = 'chat-input-file hidden'; // no accept filter — any file

        const attachBtn = document.createElement('button');
        attachBtn.type = 'button';
        attachBtn.className = 'chat-input-attach';
        attachBtn.title = 'Attach file';
        attachBtn.setAttribute('aria-label', 'Attach file');
        attachBtn.textContent = '+';

        const sendBtn = this.sendBtn = document.createElement('button');
        sendBtn.type = 'button';
        sendBtn.className = 'chat-input-send';
        sendBtn.textContent = 'Send';

        wrap.appendChild(chip);
        wrap.appendChild(attachBtn);
        wrap.appendChild(textarea);
        wrap.appendChild(fileInput);
        wrap.appendChild(sendBtn);

        attachBtn.addEventListener('click', () => fileInput.click());
        fileInput.addEventListener('change', () => {
            const file = fileInput.files && fileInput.files[0];
            fileInput.value = '';
            if (!file) return;
            this.pendingFilePath = null;
            this.renderChip(file.name, 'uploading');
            this.pendingUpload = uploadFile(this.terminalId, file)
                .then((path) => { this.pendingFilePath = path; this.renderChip(file.name, 'ready'); })
                .catch((err) => { this.renderChip(file.name, 'failed', err && err.message); })
                .finally(() => { this.pendingUpload = null; });
        });

        sendBtn.addEventListener('click', () => {
            if (this.running) this.onStop?.();
            else this.doSend();
        });
        textarea.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                if (!this.running) this.doSend();
            }
        });
    }

    // renderChip reflects the attachment state; status: uploading|ready|failed
    // or null to hide. A ready chip carries a remove control.
    renderChip(name, status, detail) {
        this.chip.textContent = '';
        if (!name) { this.chip.classList.add('hidden'); return; }
        this.chip.classList.remove('hidden');
        const icon = document.createElement('span');
        icon.className = 'chat-attach-icon'; // masked paperclip in currentColor, not an emoji
        this.chip.appendChild(icon);
        const label = document.createElement('span');
        label.className = 'chat-chip-name';
        let suffix = '';
        if (status === 'uploading') suffix = ' · uploading…';
        else if (status === 'failed') suffix = ' · failed' + (detail ? ' (' + detail + ')' : '');
        label.textContent = name + suffix;
        this.chip.appendChild(label);
        if (status === 'ready') {
            const rm = document.createElement('button');
            rm.type = 'button';
            rm.className = 'chat-chip-remove';
            rm.title = 'Remove attachment';
            rm.setAttribute('aria-label', 'Remove attachment');
            rm.textContent = '✕';
            rm.addEventListener('click', this.clearAttachment);
            this.chip.appendChild(rm);
        }
    }

    clearAttachment = () => {
        this.pendingFilePath = null;
        this.pendingUpload = null;
        this.renderChip(null);
    };

    async doSend() {
        const text = this.textarea.value.trim();
        // Wait out an in-flight upload so a fast send can't drop the file.
        if (this.pendingUpload) { try { await this.pendingUpload; } catch (e) { /* failure already shown on the chip */ } }
        if (!text && !this.pendingFilePath) return;
        this.onSend(text, this.pendingFilePath);
        this.textarea.value = '';
        this.clearAttachment();
    }

    // setRunning toggles the primary button between Send and Stop. While
    // running, the button interrupts the turn (onStop) and Enter does not send.
    setRunning = (on) => {
        this.running = on;
        this.sendBtn.textContent = on ? 'Stop' : 'Send';
        this.sendBtn.classList.toggle('chat-input-stop', on);
    };

    focus = () => this.textarea.focus();
}

export function createInputBar(opts) {
    return new InputBar(opts);
}
