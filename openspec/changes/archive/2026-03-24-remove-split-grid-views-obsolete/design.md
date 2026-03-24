## Context

The dashboard currently has three view modes (single, split, grid) with complex state management for transitions between them. This causes bugs and adds ~400 lines of code. Only single view with tabs is needed.

## Goals / Non-Goals

**Goals:**
- Remove all split and grid view code
- Simplify views.js to only handle single view with tabs
- Remove dead CSS and HTML

**Non-Goals:**
- Adding new features
- Changing tab behavior

## Decisions

### D1: Complete removal, not feature flag
**Choice:** Delete all split/grid code rather than hiding behind a flag.
**Rationale:** Dead code is maintenance burden. Can be re-added from git history if needed later.
