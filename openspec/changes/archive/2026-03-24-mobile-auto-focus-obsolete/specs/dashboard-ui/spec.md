# Spec: Dashboard UI — Mobile Input Bar (Mobile Auto-Focus)

**Spec Path:** `specs/dashboard-ui/spec.md`
**Change Type:** MODIFIED

---

## MODIFIED Requirements

### Requirement: Mobile input bar focuses automatically on session open

The mobile input bar (`#mobileInput`) SHALL receive focus automatically when a session is opened or switched to on a mobile device, so the user can begin typing immediately without tapping the input field.

#### Scenario: User opens a session on mobile

- **WHEN** the user taps a session in the session list on a mobile viewport
- **THEN** the `openSession` function is invoked
- **THEN** the `#mobileInput` element receives focus within the same user-gesture event chain
- **THEN** the virtual keyboard opens (subject to browser policy)

#### Scenario: User switches between sessions on mobile

- **WHEN** the user taps a different session tab on a mobile viewport
- **THEN** the `#mobileInput` element receives focus
- **THEN** any previously entered but unsent text in the input bar is preserved

#### Scenario: Mobile input bar is not visible

- **WHEN** the user opens a session but the mobile input bar is hidden (e.g., desktop viewport)
- **THEN** no auto-focus is attempted on `#mobileInput`
- **THEN** the desktop terminal element receives focus instead

#### Scenario: Progressive enhancement via autofocus attribute

- **WHEN** the page loads on a mobile device with no JavaScript
- **THEN** the `#mobileInput` element has the `autofocus` HTML attribute as a fallback
