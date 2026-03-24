## MODIFIED Requirements

### Requirement: tmux configuration
The container SHALL include a `.tmux.conf` at `/home/claude/.tmux.conf` with: `default-terminal` set to `xterm-256color`, `history-limit` set to `50000`, `mouse` set to `off`, `status` set to `off`, and `window-size` set to `latest`. The `smcup@:rmcup@` terminal override SHALL be set for CLI `tmux attach` users so they also get consistent behavior (no alternate screen). Mouse is disabled because the dashboard's xterm.js handles mouse events natively (selection, scroll). The dashboard's relay strips alternate screen sequences directly — the tmux override is for CLI users only.

#### Scenario: Terminal colors work correctly
- **WHEN** Claude Code runs inside a tmux session
- **THEN** 256-color output SHALL render correctly because `default-terminal` is set to `xterm-256color`

#### Scenario: Scrollback is available
- **WHEN** a tmux session has been running with extensive output
- **THEN** up to 50000 lines of scrollback SHALL be retained by tmux for CLI users

#### Scenario: No mouse reporting interference
- **WHEN** a dashboard viewer connects to a tmux session
- **THEN** tmux SHALL NOT send mouse reporting escape sequences because `mouse` is set to `off`. xterm.js handles all mouse events natively.

#### Scenario: CLI users get consistent behavior
- **WHEN** a CLI user runs `tmux attach -t <session>`
- **THEN** alternate screen SHALL be disabled via the `smcup@:rmcup@` override, giving CLI users the same scrollback behavior as dashboard viewers
