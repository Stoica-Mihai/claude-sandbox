## MODIFIED Requirements

### Requirement: Mobile input bar
On mobile (<768px), the mobile input bar SHALL be split into two rows stacked vertically:

**Row 1 (top):** Full-width text input field with clear button (X) and send button. The input SHALL occupy the maximum available width for comfortable typing.

**Row 2 (bottom):** Control buttons spread across the full width with adequate spacing for touch targets. Buttons SHALL be grouped logically: signal buttons (Esc, ^C) on the left, editing (⌫) in the middle, navigation arrows (← → ↑ ↓) on the right. Each button SHALL have a minimum tap target of 36px height for comfortable touch interaction.

#### Scenario: Two-row mobile input layout
- **WHEN** a terminal is open on a mobile viewport (<768px)
- **THEN** the mobile input bar SHALL display as two rows: the text input + send button on top, and the control buttons spread across the bottom row with visual grouping

#### Scenario: Input field has full width
- **WHEN** the mobile input bar is visible
- **THEN** the text input field SHALL span the full width of the bar (minus the send button), giving the user ample space to type and see their input

#### Scenario: Touch-friendly button sizing
- **WHEN** the user taps control buttons on the bottom row
- **THEN** each button SHALL have adequate spacing and size (minimum 36px height) to prevent accidental mis-taps on adjacent buttons
