## Context

The mobile input bar currently uses a single horizontal flex row containing 9 elements. On a 412px phone screen this leaves roughly 100px for the text input.

## Goals / Non-Goals

**Goals:**
- Give the text input full width for comfortable typing
- Make control buttons larger and easier to tap
- Group buttons logically

**Non-Goals:**
- Changing button functionality or adding new buttons
- Changing the JS logic — same functions, just different HTML layout

## Decisions

### D1: Two-row stacked layout
**Choice:** Stack two flex rows vertically inside the mobile input bar. Top row: input + send. Bottom row: all control buttons.

**Rationale:** This is the simplest change — just restructure the HTML. The input gets the full width of row 1 (minus the send button). The 7 control buttons spread across row 2 with `justify-between` or `gap` for even spacing.

### D2: Button grouping with gaps
**Choice:** Use slightly larger gaps between button groups (signals | editing | navigation) to create visual separation without adding dividers.

**Rationale:** Avoids UI clutter while making the button layout scannable.
