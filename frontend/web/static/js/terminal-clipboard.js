// Clipboard + textarea wiring for a terminal: copy-on-select, Escape re-focus,
// and image-paste upload. xterm has no built-in copyOnSelect and its paste
// handler swallows images, so both are handled here.

import { ANSI_RED, ANSI_RESET } from './terminal-ansi.js';
import { copyToClipboard, errorText } from './ui-utils.js';
import { sessionUploadPath } from './routes.js';

// Wire copy-on-select (desktop), Escape re-focus, and image-paste upload for a
// terminal. `manager` provides the live socket for sending an uploaded image path.
export function wireClipboard(containerEl, term, terminalId, manager, mobile) {
    // Copy-on-select (desktop): copy the final selection on mouseup.
    if (!mobile) {
        containerEl.addEventListener('mouseup', () => {
            const sel = term.getSelection();
            if (sel) copyToClipboard(sel); // fire-and-forget: no UI feedback here
        });
    }

    // The browser blurs the textarea on Escape below the level preventDefault can
    // stop; re-focus it from the textarea's own blur listener so Escape stays in
    // the terminal. The capture-phase paste listener runs before xterm's handler:
    // an image paste is uploaded and its path sent as input; text falls through.
    const textarea = containerEl.querySelector('.xterm-helper-textarea');
    if (!textarea) return;

    let lastKeyEscape = false;
    textarea.addEventListener('keydown', (e) => {
        lastKeyEscape = (e.key === 'Escape');
    });
    textarea.addEventListener('blur', () => {
        if (lastKeyEscape) {
            lastKeyEscape = false;
            setTimeout(() => textarea.focus(), 0);
        }
    });

    textarea.addEventListener('paste', (e) => {
        const items = e.clipboardData?.items;
        if (!items) return;
        for (const item of items) {
            if (!item.type.startsWith('image/')) continue;

            e.stopImmediatePropagation();
            e.preventDefault();

            const blob = item.getAsFile();
            if (!blob) return;

            const formData = new FormData();
            formData.append('image', blob, 'clipboard.' + blob.type.split('/')[1]);

            fetch(sessionUploadPath(terminalId), { method: 'POST', body: formData })
                .then(async (resp) => {
                    if (!resp.ok) {
                        const msg = await errorText(resp, resp.statusText);
                        term.write(`\r\n${ANSI_RED}[Upload failed: ${msg}]${ANSI_RESET}`);
                        return;
                    }
                    const { path } = await resp.json();
                    manager.instances[terminalId]?.socket?.send(new TextEncoder().encode(path));
                })
                .catch((err) => {
                    term.write(`\r\n${ANSI_RED}[Upload failed: ${err.message}]${ANSI_RESET}`);
                });
            return;
        }
    }, { capture: true });
}
