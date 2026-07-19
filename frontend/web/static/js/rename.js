// Rename Session modal: open, submit (PUT name), Enter-to-submit.

import { register } from './actions.js';
import { sendJSON } from './ui-utils.js';
import { sessionNamePath } from './routes.js';

let renameTargetId = null;

export function openRenameModal(terminalId, currentName) {
    renameTargetId = terminalId;
    const input = document.getElementById('renameInput');
    input.value = currentName || '';
    document.getElementById('renameModal').showModal();
    setTimeout(() => { input.focus(); input.select(); }, 50);
}

export function init() {
    renameTargetId = null;

    register('rename-session', (el) => openRenameModal(el.dataset.terminalId, el.dataset.name));

    document.getElementById('renameSubmit')?.addEventListener('click', () => {
        if (!renameTargetId) return;
        const name = document.getElementById('renameInput').value.trim();
        const targetId = renameTargetId;
        sendJSON(sessionNamePath(targetId), 'PUT', { name }).then(res => {
            if (!res.ok) throw new Error(`Rename failed (${res.status})`);
            document.getElementById('renameModal').close();
            renameTargetId = null;
        }).catch(err => {
            console.error('Rename failed:', err);
            document.getElementById('renameInput').classList.add('err-flash');
            setTimeout(() => {
                const el = document.getElementById('renameInput');
                if (el) el.classList.remove('err-flash');
            }, 2000);
        });
    });

    // Submit on Enter key
    document.getElementById('renameInput')?.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            document.getElementById('renameSubmit').click();
        }
    });
}
