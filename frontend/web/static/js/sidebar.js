// Sidebar: collapsible rail (rail ⇄ overlay panel) and its dismissal listeners.

// ===== Sidebar: collapsible rail (rail ⇄ overlay panel) =====
function applySidebar(expanded) {
    const side = document.getElementById('sidebar');
    const backdrop = document.getElementById('sidebarBackdrop');
    const toggle = document.getElementById('sidebarToggle');
    if (!side) return;
    side.classList.toggle('expanded', expanded);
    if (backdrop) backdrop.classList.toggle('hidden', !expanded);
    if (toggle) toggle.setAttribute('aria-expanded', String(expanded));
    localStorage.setItem('sidebar', expanded ? 'expanded' : 'collapsed');
    requestAnimationFrame(() => TerminalManager.resizeAll());
}

function toggleSidebar() {
    const side = document.getElementById('sidebar');
    applySidebar(!(side && side.classList.contains('expanded')));
}

function collapseSidebar() { applySidebar(false); }

document.getElementById('sidebarBackdrop')?.addEventListener('click', collapseSidebar);
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !document.querySelector('dialog[open]')) collapseSidebar();
});
