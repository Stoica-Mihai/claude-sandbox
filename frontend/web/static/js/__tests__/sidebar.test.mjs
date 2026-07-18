import { test } from 'node:test';
import assert from 'node:assert';
import { loadViews } from './load-views.mjs';

test('default collapsed: no expanded class, backdrop hidden', () => {
  const store = {};
  const env = loadViews({ ids: ['sidebar', 'sidebarBackdrop'], localStorage: store });
  const side = env.document.getElementById('sidebar');
  const backdrop = env.document.getElementById('sidebarBackdrop');
  assert.equal(side.classList.contains('expanded'), false);
  assert.equal(backdrop.classList.contains('hidden'), true);
});

test('toggleSidebar expands, persists, sets aria; toggle again collapses', () => {
  const store = {};
  const env = loadViews({ ids: ['sidebar', 'sidebarBackdrop', 'sidebarToggle'], localStorage: store });
  env.sandbox.toggleSidebar();
  const side = env.document.getElementById('sidebar');
  assert.equal(side.classList.contains('expanded'), true);
  assert.equal(store['sidebar'], 'expanded');
  assert.equal(env.document.getElementById('sidebarToggle').getAttribute('aria-expanded'), 'true');
  env.sandbox.toggleSidebar();
  assert.equal(side.classList.contains('expanded'), false);
  assert.equal(store['sidebar'], 'collapsed');
});

test('persisted expanded state is applied on load', () => {
  const store = { sidebar: 'expanded' };
  const env = loadViews({ ids: ['sidebar', 'sidebarBackdrop', 'sidebarToggle'], localStorage: store });
  assert.equal(env.document.getElementById('sidebar').classList.contains('expanded'), true);
});

test('collapseSidebar collapses when expanded', () => {
  const store = { sidebar: 'expanded' };
  const env = loadViews({ ids: ['sidebar', 'sidebarBackdrop'], localStorage: store });
  env.sandbox.collapseSidebar();
  assert.equal(env.document.getElementById('sidebar').classList.contains('expanded'), false);
});
