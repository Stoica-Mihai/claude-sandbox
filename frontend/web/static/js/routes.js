// API route paths injected by layout.html from shared/routes.go (single source
// with the backend route registration and the frontend proxy). The builders
// mirror the Go SessionPath/HistoryItemPath helpers, filling a pattern's
// placeholder. Read lazily (at call time) so a test that installs window.ROUTES
// after import still sees it.
function routes() {
    return (typeof window !== 'undefined' && window.ROUTES) || {};
}

function fill(pattern, key, val) {
    return (pattern || '').replace('{' + key + '}', val);
}

// Bare route paths.
export function sessionsPath() { return routes().sessions; }
export function settingsPath() { return routes().settings; }
export function uiPrefsPath() { return routes().uiPrefs; }
export function directoriesPath() { return routes().directories; }

// Concrete-path builders.
export function sessionsHistoryPath(cwd) {
    return routes().sessionsHistory + '?cwd=' + encodeURIComponent(cwd);
}
export function sessionNamePath(terminalId) { return fill(routes().sessionName, 'terminalId', terminalId); }
export function sessionUploadPath(terminalId) { return fill(routes().sessionUpload, 'terminalId', terminalId); }
export function historyItemPath(uuid) { return fill(routes().historyItem, 'uuid', encodeURIComponent(uuid)); }
export function wsTerminalPath(terminalId) { return fill(routes().wsTerminal, 'terminalId', terminalId); }
