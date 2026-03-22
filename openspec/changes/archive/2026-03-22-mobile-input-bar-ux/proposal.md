## Why

The mobile input bar crams 7 control buttons + a text input + a send button all on a single horizontal line. On a ~412px wide phone screen, the text input gets squeezed to roughly 100px, making it difficult to type and see what you're entering. The control buttons are also tiny and hard to tap accurately on touch devices.

## What Changes

- Split the mobile input bar into two rows: text input + send on top, control buttons on bottom
- Give the text input full width on its own row for comfortable typing
- Space out control buttons across the full width with larger tap targets
- Group related buttons visually (signals: Esc/^C, editing: ⌫, navigation: ←→↑↓)

## Capabilities

### Modified Capabilities
- `dashboard-ui`: Mobile input bar layout changes from single-row to two-row design

## Impact

- **layout.html**: Restructure the `#mobileInputBar` div from single flex row to two stacked rows
- **style.css**: May need minor adjustments for button spacing on the new layout
- No backend changes, no JS logic changes — same functions, just different HTML structure
