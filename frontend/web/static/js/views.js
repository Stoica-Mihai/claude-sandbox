// View mode management
// Single terminal view with tabs
let singleTerminalId = null;    // currently active tab
let singleTabs = [];            // array of open tab terminal IDs

// ===== Mobile sidebar drawer =====
function isMobile() {
    return window.matchMedia('(max-width: 767px)').matches;
}

function toggleSidebar() {
    const sidebar = document.getElementById('sidebar');
    const backdrop = document.getElementById('sidebarBackdrop');
    if (!sidebar) return;

    const isOpen = sidebar.classList.contains('sidebar-open');
    if (isOpen) {
        sidebar.classList.remove('sidebar-open');
        if (backdrop) backdrop.classList.add('hidden');
    } else {
        sidebar.classList.add('sidebar-open');
        if (backdrop) backdrop.classList.remove('hidden');
    }
}

function closeSidebar() {
    const sidebar = document.getElementById('sidebar');
    const backdrop = document.getElementById('sidebarBackdrop');
    if (!sidebar) return;
    sidebar.classList.remove('sidebar-open');
    if (backdrop) backdrop.classList.add('hidden');
}

function setView() {
    // Ensure single view is visible and resize terminals
    const viewSingle = document.getElementById('viewSingle');
    if (viewSingle) viewSingle.classList.remove('hidden');

    // Resize terminals after view switch (needs a frame for layout to settle)
    requestAnimationFrame(() => {
        TerminalManager.resizeAll();
    });
}

// Open a session terminal
function openSession(terminalId) {
    if (!terminalId) return;

    // On mobile, close the sidebar drawer
    if (isMobile()) {
        closeSidebar();
    }

    openSessionSingle(terminalId);

    // Update active state on session cards
    updateSessionCardStates();

}

function openSessionSingle(terminalId) {
    const wrapper = document.getElementById('singleTerminal');
    if (!wrapper) return;

    // If already open as a tab, just switch to it
    if (singleTabs.includes(terminalId)) {
        switchSingleTab(terminalId);
        return;
    }

    // Create a container div for this tab's terminal
    const tabContainer = document.createElement('div');
    tabContainer.id = 'singleTab-' + terminalId;
    tabContainer.className = 'absolute inset-0 hidden';
    tabContainer.classList.add('terminal-bg');
    wrapper.appendChild(tabContainer);

    // Add to tabs array
    singleTabs.push(terminalId);

    // Create the terminal in this tab's container
    TerminalManager.create(terminalId, tabContainer);

    // Switch to the new tab
    switchSingleTab(terminalId);
}

function switchSingleTab(terminalId) {
    const wrapper = document.getElementById('singleTerminal');
    if (!wrapper) return;

    // Hide all tab containers, show the selected one
    singleTabs.forEach(id => {
        const el = document.getElementById('singleTab-' + id);
        if (el) el.classList.toggle('hidden', id !== terminalId);
    });

    singleTerminalId = terminalId;

    // Update tab bar, status bar, and sidebar card highlights
    updateSingleTabBar(terminalId);

    updateSessionCardStates();

    // Focus and resize
    requestAnimationFrame(() => {
        const instance = TerminalManager.get(terminalId);
        if (instance) {
            instance.term.focus();
            TerminalManager.resize(terminalId);
        }
    });
}

function closeSingleTab(terminalId) {
    // Destroy the terminal
    TerminalManager.destroy(terminalId);

    // Remove the tab container
    const tabContainer = document.getElementById('singleTab-' + terminalId);
    if (tabContainer) tabContainer.remove();

    // Remove from tabs array
    singleTabs = singleTabs.filter(id => id !== terminalId);

    // If this was the active tab, switch to the last remaining tab or show welcome
    if (singleTerminalId === terminalId) {
        if (singleTabs.length > 0) {
            switchSingleTab(singleTabs[singleTabs.length - 1]);
        } else {
            singleTerminalId = null;
            updateSingleTabBar(null);
            updateSingleStatusBar(null);
        }
    } else {
        // Just update the tab bar to remove the closed tab
        updateSingleTabBar(singleTerminalId);
    }
    updateSessionCardStates();
}

function updateSingleWelcome(hasTerminal) {
    const welcome = document.getElementById('singleWelcome');
    const terminal = document.getElementById('singleTerminal');
    const controls = document.getElementById('singleControls');
    const mobileInput = document.getElementById('mobileInputBar');
    if (welcome) welcome.classList.toggle('hidden', hasTerminal);
    if (terminal) terminal.classList.toggle('hidden', !hasTerminal);
    if (controls) {
        // Only show controls bar on desktop (mobile has the input bar)
        if (isMobile()) {
            controls.classList.add('hidden');
        } else {
            controls.classList.toggle('hidden', !hasTerminal);
        }
    }
    if (mobileInput) mobileInput.classList.toggle('hidden', !hasTerminal);
}

// ===== Mobile control bar =====
// Send a single control byte to the active terminal
function mobileInputSend(charCode) {
    if (!singleTerminalId) return;
    const inst = TerminalManager.get(singleTerminalId);
    if (inst?.ws?.readyState === WebSocket.OPEN) {
        inst.ws.send(new Uint8Array([charCode]));
        inst.term?.scrollToBottom();
    }
}

// Send an arrow key escape sequence (\x1b[A, \x1b[B, etc.)
function mobileInputSendArrow(code) {
    if (!singleTerminalId) return;
    const inst = TerminalManager.get(singleTerminalId);
    if (inst?.ws?.readyState === WebSocket.OPEN) {
        const encoder = new TextEncoder();
        inst.ws.send(encoder.encode('\x1b[' + code));
        inst.term?.scrollToBottom();
    }
}

// Send a control byte to the active terminal in single view
// charCode: integer (e.g., 27 for Escape, 3 for Ctrl+C)
function sendKeyToTerminal(charCode) {
    if (!singleTerminalId) return;
    const inst = TerminalManager.get(singleTerminalId);
    if (inst?.ws?.readyState === WebSocket.OPEN) {
        inst.ws.send(new Uint8Array([charCode]));
        inst.term?.focus();
    }
}

// Toggle a selectable text overlay over the terminal (mobile).
function mobileToggleSelect(btn) {
    const terminal = document.getElementById('singleTerminal');
    if (!terminal) return;
    const existing = document.getElementById('selectOverlay');
    if (existing) {
        existing.remove();
        if (btn) { btn.style.color = ''; btn.style.borderColor = ''; }
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
    overlay.style.cssText = 'position:absolute;inset:0;z-index:50;margin:0;padding:12px;overflow-y:auto;-webkit-overflow-scrolling:touch;font-size:13px;font-family:monospace;line-height:1.4;color:#c9d1d9;background:#0d1117;user-select:text;-webkit-user-select:text;white-space:pre-wrap;word-break:break-all;';
    terminal.appendChild(overlay);
    // Scroll to bottom to match terminal position.
    overlay.scrollTop = overlay.scrollHeight;

    if (btn) { btn.style.color = '#3fb950'; btn.style.borderColor = 'rgba(63,185,80,0.3)'; }
}

function updateSingleTabBar(activeTerminalId) {
    const tabBar = document.getElementById('singleTabBar');
    if (!tabBar) return;

    if (singleTabs.length === 0) {
        tabBar.innerHTML = '';
        updateSingleWelcome(false);
        return;
    }

    updateSingleWelcome(true);

    tabBar.innerHTML = singleTabs.map(id => {
        const card = document.querySelector(`[data-terminal-id="${id}"]`);
        const sessionName = card ? card.dataset.session : id.substring(0, 8);
        const isActive = id === activeTerminalId;
        return `
            <div class="tab-item flex items-center gap-2 px-3 py-1.5 rounded-md text-sm cursor-pointer select-none ${isActive ? 'bg-base-300' : 'text-base-content/50 hover:text-base-content/80 hover:bg-base-300/50 transition-colors'}"
                 onclick="switchSingleTab('${escapeHtml(id)}')"
                 onauxclick="if(event.button===1){event.preventDefault();closeSingleTab('${escapeHtml(id)}');}">
                <span class="w-1.5 h-1.5 rounded-full ${isActive ? 'bg-emerald-500' : 'bg-base-content/30'}"></span>
                <span class="font-mono text-xs">${escapeHtml(sessionName)}</span>
                <button class="ml-1 opacity-40 hover:opacity-100" onclick="closeSingleTab('${escapeHtml(id)}'); event.stopPropagation();">
                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" d="M6 18L18 6M6 6l12 12"/></svg>
                </button>
            </div>`;
    }).join('');
}


function closeSingleTerminal() {
    // Close the active tab
    if (singleTerminalId) {
        closeSingleTab(singleTerminalId);
    }
    updateSessionCardStates();
}

function closeAllSingleTabs() {
    // Destroy all tabs
    [...singleTabs].forEach(id => {
        TerminalManager.destroy(id);
        const tabContainer = document.getElementById('singleTab-' + id);
        if (tabContainer) tabContainer.remove();
    });
    singleTabs = [];
    singleTerminalId = null;
    updateSingleTabBar(null);
    updateSingleStatusBar(null);
}

function updateSessionCardStates() {
    document.querySelectorAll('.session-card').forEach(card => {
        const tid = card.dataset.terminalId;
        card.classList.remove('active');

        if (singleTabs.includes(tid) && tid === singleTerminalId) {
            card.classList.add('active');
        }
    });
}

// Session card click delegation — handles all clicks on session cards
document.addEventListener('click', (e) => {
    const card = e.target.closest('.session-card');
    if (!card) return;

    // Do not intercept button clicks inside card
    if (e.target.closest('button')) return;

    const terminalId = card.dataset.terminalId;
    if (!terminalId) return;

    // Regular click: open in single view
    openSession(terminalId);
});

// Utility: escape HTML for safe insertion
function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

// Clean up terminal tab/view when a session is killed from the sidebar
function cleanupKilledSession(terminalId) {
    // Close from single view tabs
    if (singleTabs.includes(terminalId)) {
        closeSingleTab(terminalId);
    }
}

// Open the New Session modal
function openNewSessionModal(event) {
    document.getElementById('newSessionModal').showModal();
}

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
        document.getElementById('renameInput').classList.add('input-error');
        setTimeout(() => {
            document.getElementById('renameInput')?.classList.remove('input-error');
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

// Re-apply session card active states after HTMX swaps in fresh session list HTML
document.addEventListener('htmx:afterSwap', (event) => {
    if (event.target?.id === 'session-list') {
        updateSessionCardStates();
    }
});

// Listen for HTMX responses from spawn to auto-open the new terminal
document.addEventListener('htmx:afterRequest', (event) => {
    const xhr = event.detail.xhr;
    if (!xhr) return;

    const terminalId = xhr.getResponseHeader('X-Terminal-Id');
    if (!terminalId) return; // not a spawn response

    if (xhr.status >= 400) {
        const submitBtn = document.getElementById('dir-picker-submit');
        if (submitBtn) {
            submitBtn.classList.add('btn-error');
            submitBtn.textContent = 'Failed — try again';
            setTimeout(() => {
                submitBtn.classList.remove('btn-error');
                submitBtn.innerHTML = '<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5"><path stroke-linecap="round" d="M12 5v14m-7-7h14"/></svg> Launch Session';
            }, 2000);
        }
        return;
    }

    document.getElementById('newSessionModal')?.close();
    openSession(terminalId);
});

// ===== Pull to refresh =====
function initPullToRefresh() {
    const body = document.body;
    const indicator = document.getElementById('pullIndicator');
    if (!indicator) return;

    const threshold = 80;
    let startY = 0;
    let pulling = false;

    body.addEventListener('touchstart', (e) => {
        // Don't activate inside terminals, scrollback panel, open dialogs, or the sidebar drawer
        if (e.target.closest('.xterm') || e.target.closest('#singleTerminal')) return;
        if (e.target.closest('#scrollbackPanel')) return;
        if (document.querySelector('dialog[open]')) return;
        if (e.target.closest('#sidebar')) return;
        if (e.target.closest('.dropdown-content')) return;
        startY = e.touches[0].clientY;
        pulling = true;
    }, { passive: true });

    body.addEventListener('touchmove', (e) => {
        if (!pulling) return;
        const dy = e.touches[0].clientY - startY;
        if (dy < 0) { pulling = false; indicator.style.height = '0'; return; }
        const progress = Math.min(dy, threshold + 20);
        indicator.style.height = progress * 0.4 + 'px';
        indicator.textContent = dy >= threshold ? 'Release to refresh' : 'Pull to refresh';
    }, { passive: true });

    body.addEventListener('touchend', () => {
        if (!pulling) return;
        const h = parseFloat(indicator.style.height);
        if (h >= threshold * 0.4) {
            indicator.textContent = 'Refreshing...';
            setTimeout(() => location.reload(), 300);
        } else {
            indicator.style.height = '0';
        }
        pulling = false;
    });
}

// ===== Keyboard shortcuts (desktop only) =====
function handleShortcuts(e) {
    // Guard: do not fire when a modal dialog is open
    if (document.querySelector('dialog[open]')) return;

    // Guard: do not fire when focus is in an input/textarea (non-terminal)
    const tag = document.activeElement?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA') {
        if (!document.activeElement.classList.contains('xterm-helper-textarea')) {
            return;
        }
    }

    const key = e.key;

    // Alt+N — Open new session modal
    if (e.altKey && key === 'n') {
        e.preventDefault();
        openNewSessionModal({});
        return;
    }

    // Alt+W — Close current tab
    if (e.altKey && key === 'w') {
        e.preventDefault();
        if (singleTerminalId) {
            closeSingleTab(singleTerminalId);
        }
        return;
    }

    // Alt+1 through Alt+9 — Switch to tab N
    if (e.altKey) {
        const digit = parseInt(key, 10);
        if (digit >= 1 && digit <= 9) {
            e.preventDefault();
            const index = digit - 1;
            if (index < singleTabs.length) {
                switchSingleTab(singleTabs[index]);
            }
            return;
        }
    }
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    setView();
    initPullToRefresh();

    // Register keyboard shortcuts on desktop only
    if (!isMobile()) {
        document.addEventListener('keydown', handleShortcuts);
    }

    // Tick session durations every second (matches server's humanDuration format)
    setInterval(() => {
        document.querySelectorAll('.session-duration[data-created]').forEach(el => {
            const ts = parseInt(el.dataset.created, 10);
            if (!ts) return;
            let s = Math.floor(Date.now() / 1000) - ts;
            const h = Math.floor(s / 3600); s %= 3600;
            const m = Math.floor(s / 60); s %= 60;
            if (h > 0) el.textContent = m > 0 ? h + 'h ' + m + 'm' : h + 'h';
            else if (m > 0) el.textContent = s > 0 ? m + 'm ' + s + 's' : m + 'm';
            else el.textContent = s + 's';
        });
    }, 1000);
});
