## 1. Restructure mobile input bar HTML

- [ ] 1.1 Split `#mobileInputBar` inner div into two rows: top row for text input + send button, bottom row for control buttons
- [ ] 1.2 Make the text input span full width on the top row with only the send button beside it
- [ ] 1.3 Spread control buttons across the bottom row with logical grouping (Esc/^C | ⌫ | ←→↑↓) and larger tap targets (min h-9)

## 2. Verify and test

- [ ] 2.1 Verify the Go code builds cleanly
- [ ] 2.2 Test on mobile viewport (412px) with agent-browser — verify two-row layout, input has full width, buttons are spaced
- [ ] 2.3 Verify desktop is unaffected (input bar still hidden on md+)
