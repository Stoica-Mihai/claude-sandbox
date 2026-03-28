// Theme switcher — 10 themes with localStorage persistence
(function() {
    const themes = ['dark', 'light', 'synthwave', 'cupcake', 'dracula', 'forest', 'sunset', 'autumn', 'coffee', 'business'];
    const themeLabels = {
        dark: 'Dark',
        light: 'Light',
        synthwave: 'Synthwave',
        cupcake: 'Cupcake',
        dracula: 'Dracula',
        forest: 'Forest',
        sunset: 'Sunset',
        autumn: 'Autumn',
        coffee: 'Coffee',
        business: 'Business',
    };

    const savedTheme = localStorage.getItem('theme') || 'dark';
    const validTheme = themes.includes(savedTheme) ? savedTheme : 'dark';
    document.documentElement.setAttribute('data-theme', validTheme);

    // Set base attribute for CSS (light vs dark base)
    const lightThemes = ['light', 'cupcake', 'autumn'];
    document.documentElement.setAttribute('data-theme-base', lightThemes.includes(validTheme) ? 'light' : 'dark');

    // Set terminal bg CSS var early (before terminals load)
    // terminal.js defines terminalThemes — this runs after it
    if (typeof syncTerminalBgVar === 'function') {
        syncTerminalBgVar();
    }

    // Populate dropdown items
    const menu = document.getElementById('themeMenu');
    const currentLabel = document.getElementById('themeCurrentLabel');
    if (menu && currentLabel) {
        currentLabel.textContent = themeLabels[validTheme];

        menu.innerHTML = '';
        for (const t of themes) {
            const li = document.createElement('li');
            const btn = document.createElement('button');
            btn.textContent = themeLabels[t];
            btn.className = t === validTheme ? 'active' : '';
            btn.addEventListener('click', () => applyTheme(t));
            li.appendChild(btn);
            menu.appendChild(li);
        }
    }

    function applyTheme(theme) {
        document.documentElement.setAttribute('data-theme', theme);
        document.documentElement.setAttribute('data-theme-base', lightThemes.includes(theme) ? 'light' : 'dark');
        localStorage.setItem('theme', theme);

        // Update dropdown label and active state
        if (currentLabel) currentLabel.textContent = themeLabels[theme];
        if (menu) {
            const buttons = menu.querySelectorAll('button');
            themes.forEach((t, i) => {
                buttons[i].className = t === theme ? 'active' : '';
            });
        }

        // Close the dropdown
        document.activeElement?.blur();

        // Re-theme terminals
        if (typeof TerminalManager !== 'undefined') {
            TerminalManager.rethemeAll();
        }
    }
})();
