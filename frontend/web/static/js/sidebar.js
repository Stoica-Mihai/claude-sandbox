// Sidebar: collapsible rail (rail ⇄ overlay panel) and its dismissal listeners.

import { TerminalManager } from './terminal.js';
import { register } from './actions.js';

export function applySidebar(expanded) {
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

export function toggleSidebar() {
    const side = document.getElementById('sidebar');
    applySidebar(!(side && side.classList.contains('expanded')));
}

export function collapseSidebar() { applySidebar(false); }

export function init() {
    register('toggle-sidebar', () => toggleSidebar());
    // The backdrop only ever collapses — never toggles — so an off-sidebar click
    // can't re-expand it. (A backdrop wired to toggle-sidebar fought its own
    // collapse and left the sidebar open.)
    register('collapse-sidebar', () => collapseSidebar());
    document.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape' || document.querySelector('dialog[open]')) return;
        // Only dismiss an EXPANDED sidebar; otherwise a stray Escape (focused
        // session card, tab strip) triggers a pointless collapse + reflow.
        const side = document.getElementById('sidebar');
        if (side && side.classList.contains('expanded')) collapseSidebar();
    });
}
