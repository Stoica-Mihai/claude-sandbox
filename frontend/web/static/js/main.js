// Entry module: install delegated dispatch, then init every module in load order.

import { initActions } from './actions.js';
import { init as initTabs } from './tabs.js';
import { init as initSidebar } from './sidebar.js';
import { init as initMobileBar } from './mobile-bar.js';
import { init as initPicker } from './picker.js';
import { init as initRename } from './rename.js';
import { init as initTerminal } from './terminal.js';
import { init as initTheme } from './theme.js';
import { init as initSettings } from './settings.js';
import { init as initShare } from './share.js';
import { init as initApp } from './app-init.js';

function boot() {
    initActions();

    initSidebar();
    initTabs();
    initMobileBar();
    initPicker();
    initRename();
    initTerminal();
    initTheme();
    initSettings();
    initShare();
    initApp();
}

if (typeof document !== 'undefined') {
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', boot);
    } else {
        boot();
    }
}
