// Rename Session modal: open, submit (PUT name), Enter-to-submit.

// Open the Rename Session modal
let renameTargetId = null;
function openRenameModal(terminalId, currentName) {
    renameTargetId = terminalId;
    const input = document.getElementById('renameInput');
    input.value = currentName || '';
    document.getElementById('renameModal').showModal();
    setTimeout(() => { input.focus(); input.select(); }, 50);
}
document.getElementById('renameSubmit')?.addEventListener('click', () => {
    if (!renameTargetId) return;
    const name = document.getElementById('renameInput').value.trim();
    const targetId = renameTargetId;
    fetch(`/api/sessions/${targetId}/name`, {
        method: 'PUT',
        body: JSON.stringify({ name })
    }).then(res => {
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
