'use strict';

// Covers the delegated-dispatch mechanism (actions.js): a single document click
// listener resolves the nearest [data-action] ancestor and calls its handler.
// Without this, the whole inline-onclick replacement is untested.

const test = require('node:test');
const assert = require('node:assert/strict');
const { FakeDocument, FakeElement } = require('./dom-stub');
const actions = require('../actions.js');

function freshDoc() {
    const document = new FakeDocument();
    globalThis.document = document;
    actions.initActions();
    return document;
}

test('a registered handler fires on a click whose target carries data-action', () => {
    const document = freshDoc();
    let got = null;
    actions.register('do-thing', (el) => { got = el.dataset.value; });
    const el = new FakeElement('button');
    el.dataset.action = 'do-thing';
    el.dataset.value = 'x';
    document.dispatch('click', { target: el });
    assert.equal(got, 'x', 'handler ran with the data-action element');
});

test('a click on a nested child resolves to the nearest data-action ancestor', () => {
    const document = freshDoc();
    let fired = false;
    actions.register('ancestor', () => { fired = true; });
    const parent = new FakeElement('div');
    parent.dataset.action = 'ancestor';
    const child = new FakeElement('span');
    child.parentNode = parent;
    document.dispatch('click', { target: child });
    assert.ok(fired, 'closest() walked up to the ancestor handler');
});

test('a click with no data-action ancestor is a no-op', () => {
    const document = freshDoc();
    const loose = new FakeElement('div');
    assert.doesNotThrow(() => document.dispatch('click', { target: loose }));
});

test('an unregistered data-action name is ignored', () => {
    const document = freshDoc();
    const el = new FakeElement('button');
    el.dataset.action = 'nope-not-registered';
    assert.doesNotThrow(() => document.dispatch('click', { target: el }));
});
