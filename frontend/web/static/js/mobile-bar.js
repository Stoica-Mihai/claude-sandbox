// Mobile control bar: send keys/bytes to the active terminal, select overlay.

import { singleTerminalId } from './tabs.js';
import { TerminalManager } from './terminal.js';
import { register } from './actions.js';

// Control-byte codes for the terminal control buttons.
export const KEY_ESCAPE = 27;
export const KEY_CTRL_C = 3;
export const KEY_CTRL_D = 4;
export const KEY_BACKSPACE = 127;

// data-key value → control byte, for the delegated send-key action.
const KEY_BYTES = {
    'escape': KEY_ESCAPE,
    'ctrl-c': KEY_CTRL_C,
    'ctrl-d': KEY_CTRL_D,
    'backspace': KEY_BACKSPACE,
};

// Send bytes to the active terminal's socket and scroll it to the bottom.
// Returns the instance (for callers that also focus) or null if none is ready.
export function sendToActiveTerminal(bytes) {
    if (!singleTerminalId) return null;
    const inst = TerminalManager.get(singleTerminalId);
    if (inst?.socket?.send(bytes)) {
        inst.term?.scrollToBottom();
        return inst;
    }
    return null;
}

// Send a single control byte to the active terminal (and refocus it).
export function sendKeyToTerminal(charCode) {
    const inst = sendToActiveTerminal(new Uint8Array([charCode]));
    inst?.term?.focus();
}

// Send an arrow key escape sequence (\x1b[A, \x1b[B, etc.)
export function mobileInputSendArrow(code) {
    sendToActiveTerminal(new TextEncoder().encode('\x1b[' + code));
}

// Toggle a selectable text overlay over the terminal (mobile).
export function mobileToggleSelect(btn) {
    const terminal = document.getElementById('singleTerminal');
    if (!terminal) return;
    const existing = document.getElementById('selectOverlay');
    if (existing) {
        existing.remove();
        if (btn) btn.classList.remove('sel-active');
        return;
    }

    if (!singleTerminalId) return;
    const inst = TerminalManager.get(singleTerminalId);
    if (!inst) return;

    // Extract visible lines.
    const buf = inst.term.buffer.active;
    const totalLines = buf.baseY + inst.term.rows;
    const lines = [];
    for (let i = 0; i <= totalLines; i++) {
        const line = buf.getLine(i);
        lines.push(line ? line.translateToString(true) : '');
    }
    while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop();

    const overlay = document.createElement('pre');
    overlay.id = 'selectOverlay';
    overlay.textContent = lines.join('\n');
    terminal.appendChild(overlay);
    // Scroll to bottom to match terminal position.
    overlay.scrollTop = overlay.scrollHeight;

    if (btn) btn.classList.add('sel-active');
}

export function init() {
    register('send-key', (el) => sendKeyToTerminal(KEY_BYTES[el.dataset.key]));
    register('arrow', (el) => mobileInputSendArrow(el.dataset.dir));
    register('toggle-select', (el) => mobileToggleSelect(el));
}
