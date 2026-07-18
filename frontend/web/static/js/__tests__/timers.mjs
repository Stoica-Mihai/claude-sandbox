// Fake timer harness shared by the sandbox loaders: queue setTimeout callbacks
// and fire the pending ones on flush().
function makeTimers() {
    const pending = [];
    return {
        pending,
        setTimeout: (fn, ms) => { pending.push({ fn, ms }); return pending.length; },
        clearTimeout: () => {},
        flush() {
            const due = pending.splice(0);
            due.forEach(t => t.fn());
        },
    };
}

export { makeTimers };
