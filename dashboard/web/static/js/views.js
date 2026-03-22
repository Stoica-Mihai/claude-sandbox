// View mode management
// Tracks which terminal is open in each pane
let currentView = localStorage.getItem('viewMode') || 'single';
let singleTerminalId = null;    // currently active tab
let singleTabs = [];            // array of open tab terminal IDs
let splitLeftTerminalId = null;
let splitRightTerminalId = null;
let spawnToRightPane = false; // When true, next spawned session opens in split right pane
let savedSplitLeft = null;  // Remember split state when switching away
let savedSplitRight = null;

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

function setView(view) {
    // On mobile, force single view — split/grid not supported
    if (isMobile() && (view === 'split' || view === 'grid')) {
        view = 'single';
    }

    const prevView = currentView;
    currentView = view;
    localStorage.setItem('viewMode', view);

    // When leaving split for single/grid: save split state, destroy split terminals,
    // and open the left pane's session in single view
    if (prevView === 'split' && view !== 'split') {
        if (splitLeftTerminalId || splitRightTerminalId) {
            savedSplitLeft = splitLeftTerminalId;
            savedSplitRight = splitRightTerminalId;
        }

        // Destroy split xterm instances (PTYs stay alive)
        if (splitLeftTerminalId) TerminalManager.destroy(splitLeftTerminalId);
        if (splitRightTerminalId) TerminalManager.destroy(splitRightTerminalId);
        const lc = document.getElementById('splitLeftTerminal');
        const rc = document.getElementById('splitRightTerminal');
        if (lc) lc.innerHTML = '';
        if (rc) rc.innerHTML = '';

        // Open the left pane's session in single view (if switching to single)
        if (view === 'single' && savedSplitLeft) {
            splitLeftTerminalId = null;
            splitRightTerminalId = null;
            // Defer so the single view container is visible first
            requestAnimationFrame(() => {
                openSessionSingle(savedSplitLeft);
            });
        } else {
            splitLeftTerminalId = null;
            splitRightTerminalId = null;
        }
    }

    // When leaving single for split: close tabs that will be restored in split panes
    if (prevView === 'single' && view === 'split') {
        // Close tabs that overlap with saved split sessions (they'll be recreated in panes)
        [savedSplitLeft, savedSplitRight].filter(Boolean).forEach(id => {
            if (singleTabs.includes(id)) {
                closeSingleTab(id);
            }
        });
    }

    // Update view button active states
    document.querySelectorAll('.view-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.view === view);
    });

    // Toggle view containers
    const viewSingle = document.getElementById('viewSingle');
    const viewSplit = document.getElementById('viewSplit');
    const viewGrid = document.getElementById('viewGrid');

    viewSingle.classList.toggle('hidden', view !== 'single');
    viewSplit.classList.toggle('hidden', view !== 'split');
    if (view === 'split') {
        viewSplit.style.display = 'flex';
    } else {
        viewSplit.style.display = '';
    }
    viewGrid.classList.toggle('hidden', view !== 'grid');

    // Show/hide split hint in sidebar
    const splitHint = document.getElementById('splitHint');
    if (splitHint) {
        splitHint.classList.toggle('hidden', view !== 'split');
    }

    // When switching to split, restore saved sessions or reset pane headers
    if (view === 'split') {
        // Restore saved split state if available
        if (!splitLeftTerminalId && savedSplitLeft) {
            openSessionInPane(savedSplitLeft, 'left');
        }
        if (!splitRightTerminalId && savedSplitRight) {
            openSessionInPane(savedSplitRight, 'right');
        }
        // Keep saved state — it'll be overwritten next time we leave split

        // Reset headers for any still-empty panes
        if (!splitLeftTerminalId) {
            const label = document.getElementById('splitLeftLabel');
            const dot = document.getElementById('splitLeftDot');
            const status = document.getElementById('splitLeftStatus');
            if (label) label.textContent = 'Left pane';
            if (dot) dot.className = 'w-1.5 h-1.5 rounded-full bg-base-content/20';
            if (status) status.textContent = 'Click a session to open here';
        }
        if (!splitRightTerminalId) {
            const label = document.getElementById('splitRightLabel');
            const dot = document.getElementById('splitRightDot');
            const status = document.getElementById('splitRightStatus');
            if (label) label.textContent = 'Right pane';
            if (dot) dot.className = 'w-1.5 h-1.5 rounded-full bg-base-content/20';
            if (status) status.textContent = 'Shift+click a session to open here';
        }
    }

    // Resize terminals after view switch (needs a frame for layout to settle)
    requestAnimationFrame(() => {
        TerminalManager.resizeAll();
    });

    // If switching to grid, rebuild grid cards
    if (view === 'grid') {
        buildGridView();
    }
}

// Open a session terminal in the current view's appropriate container
function openSession(terminalId) {
    if (!terminalId) return;

    // On mobile, close the sidebar drawer
    if (isMobile()) {
        closeSidebar();
    }

    switch (currentView) {
        case 'single':
            openSessionSingle(terminalId);
            break;
        case 'split':
            openSessionInPane(terminalId, 'left');
            break;
        case 'grid':
            // Switch to single view and open there
            setView('single');
            openSessionSingle(terminalId);
            break;
    }

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
    tabContainer.style.backgroundColor = '#0d1117';
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
    updateSingleStatusBar(terminalId);
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

function openSessionInPane(terminalId, pane) {
    const containerId = pane === 'left' ? 'splitLeftTerminal' : 'splitRightTerminal';
    const container = document.getElementById(containerId);
    if (!container) return;

    const currentId = pane === 'left' ? splitLeftTerminalId : splitRightTerminalId;

    // If same terminal already open, just focus
    if (currentId === terminalId && TerminalManager.get(terminalId)) {
        TerminalManager.get(terminalId).term.focus();
        return;
    }

    // Destroy previous terminal in this container
    if (currentId) {
        const prev = TerminalManager.getByContainer(containerId);
        if (prev) {
            TerminalManager.destroy(prev.terminalId);
        }
    }

    // Clear container
    container.innerHTML = '';

    if (pane === 'left') {
        splitLeftTerminalId = terminalId;
    } else {
        splitRightTerminalId = terminalId;
    }

    TerminalManager.create(terminalId, container);

    // Update pane header
    updateSplitPaneHeader(terminalId, pane);

    // Focus and resize
    requestAnimationFrame(() => {
        const instance = TerminalManager.get(terminalId);
        if (instance) {
            instance.term.focus();
            TerminalManager.resize(terminalId);
        }
    });

    updateSessionCardStates();
}

function openSessionRight(terminalId, event) {
    if (event) {
        event.preventDefault();
    }

    // If not in split view, switch to it
    if (currentView !== 'split') {
        // If there was a single terminal open, move it to the left pane
        if (currentView === 'single' && singleTerminalId) {
            const prevId = singleTerminalId;
            setView('split');
            openSessionInPane(prevId, 'left');
        } else {
            setView('split');
        }
    }

    openSessionInPane(terminalId, 'right');
}

function closeSplitPane(pane) {
    const containerId = pane === 'left' ? 'splitLeftTerminal' : 'splitRightTerminal';
    const container = document.getElementById(containerId);

    if (pane === 'left' && splitLeftTerminalId) {
        TerminalManager.destroy(splitLeftTerminalId);
        splitLeftTerminalId = null;
    } else if (pane === 'right' && splitRightTerminalId) {
        TerminalManager.destroy(splitRightTerminalId);
        splitRightTerminalId = null;
    }

    if (container) {
        container.innerHTML = '';
    }

    // Reset pane header
    const labelId = pane === 'left' ? 'splitLeftLabel' : 'splitRightLabel';
    const dotId = pane === 'left' ? 'splitLeftDot' : 'splitRightDot';
    const statusId = pane === 'left' ? 'splitLeftStatus' : 'splitRightStatus';

    const label = document.getElementById(labelId);
    const dot = document.getElementById(dotId);
    const status = document.getElementById(statusId);

    if (label) label.textContent = pane.charAt(0).toUpperCase() + pane.slice(1) + ' pane';
    if (dot) { dot.className = 'w-1.5 h-1.5 rounded-full bg-base-content/20'; }
    if (status) status.textContent = 'No session';

    updateSessionCardStates();
}

function updateSingleWelcome(hasTerminal) {
    const welcome = document.getElementById('singleWelcome');
    const terminal = document.getElementById('singleTerminal');
    const statusBar = document.getElementById('singleStatusBar');
    const controls = document.getElementById('singleControls');
    const mobileInput = document.getElementById('mobileInputBar');
    if (welcome) welcome.classList.toggle('hidden', hasTerminal);
    if (terminal) terminal.classList.toggle('hidden', !hasTerminal);
    if (statusBar) statusBar.classList.toggle('hidden', !hasTerminal);
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

// ===== Mobile input bar =====
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

// Submit the text from the mobile input field
function mobileInputSubmit() {
    const input = document.getElementById('mobileInput');
    if (!input || !singleTerminalId) return;
    const text = input.value;
    if (!text) return;

    const inst = TerminalManager.get(singleTerminalId);
    if (inst?.ws?.readyState === WebSocket.OPEN) {
        const encoder = new TextEncoder();
        // Send the text followed by Enter
        inst.ws.send(encoder.encode(text + '\r'));
        inst.term?.scrollToBottom();
    }
    input.value = '';
    input.focus();
}

// Handle keydown in the mobile input — Enter submits
function mobileInputKeydown(event) {
    if (event.key === 'Enter') {
        event.preventDefault();
        mobileInputSubmit();
    }
}

// Show/hide the clear button based on input content
function mobileInputToggleClear() {
    const input = document.getElementById('mobileInput');
    const clearBtn = document.getElementById('mobileInputClear');
    if (input && clearBtn) {
        clearBtn.classList.toggle('hidden', !input.value);
    }
}

// Clear the mobile input text
function mobileInputClearText() {
    const input = document.getElementById('mobileInput');
    if (input) {
        input.value = '';
        input.focus();
        mobileInputToggleClear();
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

// Send a control byte to a split pane's terminal
function sendKeyToPane(pane, charCode) {
    const terminalId = pane === 'left' ? splitLeftTerminalId : splitRightTerminalId;
    if (!terminalId) return;
    const inst = TerminalManager.get(terminalId);
    if (inst?.ws?.readyState === WebSocket.OPEN) {
        inst.ws.send(new Uint8Array([charCode]));
        inst.term?.focus();
    }
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
                 onclick="switchSingleTab('${escapeHtml(id)}')">
                <span class="w-1.5 h-1.5 rounded-full ${isActive ? 'bg-emerald-500' : 'bg-base-content/30'}"></span>
                <span class="font-mono text-xs">${escapeHtml(sessionName)}</span>
                <button class="ml-1 opacity-40 hover:opacity-100" onclick="closeSingleTab('${escapeHtml(id)}'); event.stopPropagation();">
                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" d="M6 18L18 6M6 6l12 12"/></svg>
                </button>
            </div>`;
    }).join('');
}

function updateSingleStatusBar(terminalId) {
    const leftEl = document.getElementById('singleStatusLeft');
    const rightEl = document.getElementById('singleStatusRight');

    if (!terminalId) {
        if (leftEl) leftEl.innerHTML = '';
        if (rightEl) rightEl.textContent = '';
        return;
    }

    const card = document.querySelector(`[data-terminal-id="${terminalId}"]`);

    if (card && leftEl) {
        const cwd = card.querySelector('.font-medium')?.textContent || '';
        const pid = card.querySelector('.font-mono.text-xs')?.textContent || '';
        leftEl.innerHTML = `<span>${escapeHtml(pid)}</span><span class="ml-4">${escapeHtml(terminalId.substring(0, 6))}</span><span class="ml-4">${escapeHtml(cwd)}</span>`;
    }

    if (rightEl) {
        rightEl.textContent = '';
    }
}

function updateSplitPaneHeader(terminalId, pane) {
    const card = document.querySelector(`[data-terminal-id="${terminalId}"]`);
    const sessionName = card ? card.dataset.session : terminalId;

    const labelId = pane === 'left' ? 'splitLeftLabel' : 'splitRightLabel';
    const dotId = pane === 'left' ? 'splitLeftDot' : 'splitRightDot';
    const statusId = pane === 'left' ? 'splitLeftStatus' : 'splitRightStatus';

    const label = document.getElementById(labelId);
    const dot = document.getElementById(dotId);
    const status = document.getElementById(statusId);

    if (label) label.textContent = sessionName;
    if (dot) { dot.className = 'w-1.5 h-1.5 rounded-full bg-emerald-500'; }

    if (status && card) {
        const cwd = card.querySelector('.font-medium')?.textContent || '';
        const pid = card.querySelector('.font-mono.text-xs')?.textContent || '';
        status.textContent = `${pid} \u00b7 ${cwd}`;
    }
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
        card.classList.remove('active', 'in-split');

        if (currentView === 'single' && singleTabs.includes(tid)) {
            // Highlight the focused tab, subtle highlight for other open tabs
            if (tid === singleTerminalId) {
                card.classList.add('active');
            }
        } else if (currentView === 'split') {
            if (tid === splitLeftTerminalId) {
                card.classList.add('active');
            }
            if (tid === splitRightTerminalId) {
                card.classList.add('in-split');
            }
        }
    });
}

// Build grid view from session cards
function buildGridView() {
    const gridContainer = document.getElementById('gridContainer');
    if (!gridContainer) return;

    const cards = document.querySelectorAll('#session-list .session-card');
    gridContainer.innerHTML = '';

    cards.forEach(card => {
        const tid = card.dataset.terminalId;
        const sessionName = card.dataset.session || 'unknown';
        const cwd = card.querySelector('.font-medium')?.textContent || '';
        const pid = card.querySelector('.font-mono.text-xs')?.textContent || '';
        const duration = card.querySelector('.text-\\[10px\\]')?.textContent || '';
        const isExternal = card.classList.contains('opacity-60');
        const badge = card.querySelector('.badge');
        const badgeHtml = badge ? badge.outerHTML : '';

        const gridCard = document.createElement('div');
        gridCard.className = 'grid-card card bg-base-200 border border-base-content/10 cursor-pointer' + (isExternal ? ' border-dashed opacity-60' : '');

        if (!isExternal) {
            gridCard.onclick = () => {
                setView('single');
                openSession(tid);
            };
        }

        gridCard.innerHTML = `
            <div class="card-body p-4">
                <div class="flex items-center justify-between mb-2">
                    <div class="flex items-center gap-2">
                        <span class="w-2 h-2 rounded-full ${isExternal ? 'bg-warning' : 'bg-emerald-500 pulse-alive'}"></span>
                        <span class="font-semibold text-sm">${escapeHtml(sessionName)}</span>
                        ${isExternal ? '<span class="badge badge-xs badge-warning badge-outline">external</span>' : ''}
                    </div>
                    <span class="font-mono text-[10px] text-base-content/30">${escapeHtml(pid)} \u00b7 ${escapeHtml(duration)}</span>
                </div>
                ${isExternal
                    ? '<div class="bg-base-300/50 rounded-lg p-3 h-28 flex items-center justify-center"><span class="text-xs text-base-content/25">External session \u2014 no terminal access</span></div>'
                    : '<div class="bg-[#0d1117] rounded-lg p-3 text-[10px] font-mono leading-relaxed text-base-content/50 h-28 overflow-hidden flex items-center justify-center"><span class="text-base-content/25">Click to open terminal</span></div>'
                }
            </div>
        `;

        gridContainer.appendChild(gridCard);
    });

    // Add "New Session" card
    const newCard = document.createElement('div');
    newCard.className = 'grid-card card bg-base-200 border border-base-content/10 border-dashed cursor-pointer hover:border-primary/30';
    newCard.onclick = () => document.getElementById('newSessionModal').showModal();
    newCard.innerHTML = `
        <div class="card-body p-4 flex items-center justify-center">
            <div class="text-center">
                <svg class="w-8 h-8 text-base-content/20 mx-auto mb-2" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" d="M12 5v14m-7-7h14"/></svg>
                <span class="text-sm text-base-content/30">New Session</span>
            </div>
        </div>
    `;
    gridContainer.appendChild(newCard);
}

// Split divider drag handling
function initSplitDivider() {
    const divider = document.getElementById('splitDivider');
    if (!divider) return;

    let isDragging = false;

    divider.addEventListener('mousedown', (e) => {
        isDragging = true;
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
        e.preventDefault();
    });

    document.addEventListener('mousemove', (e) => {
        if (!isDragging) return;

        const splitView = document.getElementById('viewSplit');
        if (!splitView) return;

        const rect = splitView.getBoundingClientRect();
        const leftPercent = ((e.clientX - rect.left) / rect.width) * 100;

        // Clamp between 20% and 80%
        const clamped = Math.max(20, Math.min(80, leftPercent));

        const leftPane = splitView.querySelector('.terminal-pane:first-child');
        const rightPane = splitView.querySelector('.terminal-pane:last-child');

        if (leftPane && rightPane) {
            leftPane.style.flex = `0 0 ${clamped}%`;
            rightPane.style.flex = `0 0 ${100 - clamped}%`;
        }

        // Debounce terminal resize during drag
        clearTimeout(divider._resizeTimeout);
        divider._resizeTimeout = setTimeout(() => {
            TerminalManager.resizeAll();
        }, 50);
    });

    document.addEventListener('mouseup', () => {
        if (!isDragging) return;
        isDragging = false;
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        TerminalManager.resizeAll();
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

    // External sessions are not clickable
    if (card.classList.contains('opacity-60')) return;

    if (e.shiftKey) {
        // Shift+click: open in split right pane (auto-switches to split view)
        e.preventDefault();
        openSessionRight(terminalId, e);
    } else {
        // Regular click: open in current view
        openSession(terminalId);
    }
});

// Utility: escape HTML for safe insertion
function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

// Maximize a split pane — switch to single view with that pane's terminal
function maximizeSplitPane(pane) {
    const terminalId = pane === 'left' ? splitLeftTerminalId : splitRightTerminalId;
    if (!terminalId) {
        setView('single');
        return;
    }

    // Save split state before destroying anything
    savedSplitLeft = splitLeftTerminalId;
    savedSplitRight = splitRightTerminalId;

    // Destroy both pane terminals (they'll be recreated when needed)
    if (splitLeftTerminalId) TerminalManager.destroy(splitLeftTerminalId);
    if (splitRightTerminalId) TerminalManager.destroy(splitRightTerminalId);

    const leftContainer = document.getElementById('splitLeftTerminal');
    const rightContainer = document.getElementById('splitRightTerminal');
    if (leftContainer) leftContainer.innerHTML = '';
    if (rightContainer) rightContainer.innerHTML = '';

    // Clear split state
    splitLeftTerminalId = null;
    splitRightTerminalId = null;

    // Switch to single and open the maximized terminal
    setView('single');
    openSessionSingle(terminalId);
}

// Clean up terminal tab/view when a session is killed from the sidebar
function cleanupKilledSession(terminalId) {
    // Close from single view tabs
    if (singleTabs.includes(terminalId)) {
        closeSingleTab(terminalId);
    }

    // Clear from split panes
    if (splitLeftTerminalId === terminalId) {
        TerminalManager.destroy(terminalId);
        splitLeftTerminalId = null;
        const container = document.getElementById('splitLeftTerminal');
        if (container) container.innerHTML = '';
    }
    if (splitRightTerminalId === terminalId) {
        TerminalManager.destroy(terminalId);
        splitRightTerminalId = null;
        const container = document.getElementById('splitRightTerminal');
        if (container) container.innerHTML = '';
    }
}

// Open the New Session modal — Shift+Click sets spawnToRightPane flag
function openNewSessionModal(event) {
    spawnToRightPane = event && event.shiftKey;
    document.getElementById('newSessionModal').showModal();
}

// Re-apply session card active states after HTMX swaps in fresh session list HTML
document.addEventListener('htmx:afterSwap', (event) => {
    if (event.target?.id === 'session-list') {
        updateSessionCardStates();
    }
});

// Listen for HTMX responses from spawn to auto-open the new terminal
document.addEventListener('htmx:afterRequest', (event) => {
    // Only handle POST /api/sessions (spawn)
    if (event.detail.verb !== 'post' || !event.detail.pathInfo?.requestPath?.includes('/api/sessions')) return;

    const terminalId = event.detail.xhr?.getResponseHeader('X-Terminal-Id');
    if (!terminalId) return;

    if (spawnToRightPane) {
        spawnToRightPane = false;
        openSessionRight(terminalId);
    } else {
        openSession(terminalId);
    }
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
        // Don't activate inside terminals
        if (e.target.closest('.xterm') || e.target.closest('#singleTerminal') ||
            e.target.closest('#splitLeftTerminal') || e.target.closest('#splitRightTerminal')) return;
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

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    // On mobile, force single view regardless of stored preference
    if (isMobile() && (currentView === 'split' || currentView === 'grid')) {
        currentView = 'single';
    }
    setView(currentView);
    initSplitDivider();
    initPullToRefresh();

    // When resizing from desktop to mobile, enforce single view and close sidebar
    window.matchMedia('(max-width: 767px)').addEventListener('change', (e) => {
        if (e.matches) {
            // Entered mobile: force single view, close sidebar
            if (currentView !== 'single') {
                setView('single');
            }
            closeSidebar();
        }
    });
});
