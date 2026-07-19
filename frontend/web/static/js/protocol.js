// WS control-message type vocabulary, injected by layout.html from the shared
// Go contract (window.WS_CONTROL). Reading it here means the browser speaks the
// same control protocol as sessiond without re-typing the literal strings — a
// rename on the Go side flows through the injection. Read lazily (at call time)
// so a test that installs the global after import still sees it.
export function wsControl() {
    const c = (typeof window !== 'undefined' && window.WS_CONTROL) || {};
    return {
        RESIZE: c.resize,
        DEACTIVATED: c.deactivated,
        REACTIVATE: c.reactivate,
        ERROR: c.error,
    };
}
