// NEW SESSION modal's Terminal/Chat mode-choice toggle: persisted preference
// (localStorage), synced onto the hidden #dir-picker-kind form field, applied
// both on init and after every directory fragment swap (dpResetBrowse).
//
// The toggle buttons' own active-class state (`.mode-opt.active`) is not
// asserted here: FakeDocument.querySelectorAll is a no-op stub shared by every
// view test (see dom-stub.mjs), and widening it to a real tree search risks
// changing behavior for unrelated existing tests that also call
// document.querySelectorAll (tabs.js's session-card/duration updates,
// settings.js). That visual state is covered by the manual smoke test instead.
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { loadViews, FakeElement } from './load-views.mjs';

function idsWithKindInput() {
    return ['dir-picker-kind', 'dir-picker-cwd', 'dir-picker-resume', 'dir-picker', 'session-actions', 'dp-folders'];
}

test('init defaults the hidden kind field to terminal with no stored preference', () => {
    const env = loadViews({ ids: idsWithKindInput() });
    assert.equal(env.document.getElementById('dir-picker-kind').value, 'terminal');
});

test('init restores a stored chat preference onto the hidden kind field', () => {
    const env = loadViews({ ids: idsWithKindInput(), localStorage: { spawnSessionKind: 'chat' } });
    assert.equal(env.document.getElementById('dir-picker-kind').value, 'chat');
});

test('an unrecognized stored value falls back to terminal', () => {
    const env = loadViews({ ids: idsWithKindInput(), localStorage: { spawnSessionKind: 'bogus' } });
    assert.equal(env.document.getElementById('dir-picker-kind').value, 'terminal');
});

test("the 'pick-session-kind' action switches the hidden field and persists the choice", () => {
    const env = loadViews({ ids: idsWithKindInput() });
    env.sandbox.initActions();

    const btn = new FakeElement('button');
    btn.dataset.action = 'pick-session-kind';
    btn.dataset.kind = 'chat';
    env.document.body.appendChild(btn);

    env.document.dispatch('click', { target: btn });

    assert.equal(env.document.getElementById('dir-picker-kind').value, 'chat');
    assert.equal(env.sandbox.localStorage.getItem('spawnSessionKind'), 'chat');
});

test('dpResetBrowse (fired by a directory fragment swap) re-applies the current preference to a fresh hidden field', () => {
    const env = loadViews({ ids: idsWithKindInput(), localStorage: { spawnSessionKind: 'chat' } });

    // Simulate the fragment swap replacing #dir-picker's hidden kind input
    // with a fresh, default-valued one (as the real HTML template does).
    const fresh = new FakeElement('input');
    env.document.register('dir-picker-kind', fresh);
    fresh.value = 'terminal';

    env.document.dispatch('htmx:afterSwap', { target: env.document.getElementById('dir-picker') });

    assert.equal(env.document.getElementById('dir-picker-kind').value, 'chat');
});
