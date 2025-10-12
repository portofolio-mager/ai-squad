package app

import (
	"ai-squad/log"
	"ai-squad/session"
	"ai-squad/ui"
	"ai-squad/ui/overlay"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type helpText interface {
	// toContent returns the help UI content.
	toContent() string
	// mask returns the bit mask for this help text. These are used to track which help screens
	// have been seen in the config and app state.
	mask() uint32
}

type helpTypeGeneral struct{}

type helpTypeInstanceStart struct {
	instance *session.Instance
}

type helpTypeInstanceAttach struct{}

type helpTypeInstanceCheckout struct{}

func helpStart(instance *session.Instance) helpText {
	return helpTypeInstanceStart{instance: instance}
}

func (h helpTypeGeneral) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("AI Squad"),
		"",
		"A terminal UI that manages multiple Claude Code (and other local agents) in separate workspaces.",
		"",
		headerStyle.Render("Managing Sessions:"),
		keyStyle.Render("n")+descStyle.Render("         - Create a new session"),
		keyStyle.Render("N")+descStyle.Render("         - Create a new session with a prompt"),
		keyStyle.Render("e")+descStyle.Render("         - Create session from existing branch"),
		keyStyle.Render("D")+descStyle.Render("         - Kill (delete) the selected session"),
		keyStyle.Render("↑/k, ↓/j")+descStyle.Render("  - Navigate between sessions"),
		keyStyle.Render("↵/o")+descStyle.Render("       - Attach to the selected session"),
		keyStyle.Render("ctrl-q")+descStyle.Render("    - Detach from session"),
		keyStyle.Render("a/P")+descStyle.Render("       - Change program for the selected session (both 'a' and 'P')"),
		keyStyle.Render("l")+descStyle.Render("         - List available/current programs"),
		"",
		headerStyle.Render("Git & Handoff:"),
		keyStyle.Render("p")+descStyle.Render("         - Commit and push branch to GitHub"),
		keyStyle.Render("c")+descStyle.Render("         - Checkout: commit changes and pause session"),
		keyStyle.Render("r")+descStyle.Render("         - Resume a paused session"),
		keyStyle.Render("b")+descStyle.Render("         - Rebase with main branch"),
		keyStyle.Render("h")+descStyle.Render("         - Git reset --hard to origin/branch"),
		keyStyle.Render("B")+descStyle.Render("         - Create bookmark commit"),
		keyStyle.Render("g")+descStyle.Render("         - Show git status"),
		keyStyle.Render("G")+descStyle.Render("         - Show git status bookmarks"),
		"",
		headerStyle.Render("IDE & Tools:"),
		keyStyle.Render("w")+descStyle.Render("         - Open current instance in IDE"),
		keyStyle.Render("i")+descStyle.Render("         - Open current file in IDE (diff view)"),
		keyStyle.Render("x")+descStyle.Render("         - Open in external diff tool"),
		keyStyle.Render("t")+descStyle.Render("         - Run tests"),
		keyStyle.Render("R")+descStyle.Render("         - Review PR comments"),
		keyStyle.Render("ctrl+r")+descStyle.Render("    - Resolve all PR conversations"),
		"",
		headerStyle.Render("Navigation:"),
		keyStyle.Render("tab")+descStyle.Render("       - Switch between AI, diff, and terminal tabs"),
		keyStyle.Render("shift-↓/↑")+descStyle.Render(" - Scroll in diff view"),
		keyStyle.Render("s")+descStyle.Render("         - Toggle scroll lock (↓/↑ scrolls diff)"),
		keyStyle.Render("home/end")+descStyle.Render("  - Scroll to top/bottom"),
		keyStyle.Render("ctrl+a/e")+descStyle.Render("  - Alternative: scroll to top/bottom"),
		keyStyle.Render("pgup/pgdn")+descStyle.Render(" - Page up/down"),
		keyStyle.Render("alt-↓/↑")+descStyle.Render("   - Jump to next/prev file in diff"),
		keyStyle.Render("a")+descStyle.Render("         - Show all changes in diff"),
		keyStyle.Render("d")+descStyle.Render("         - Show commit history"),
		keyStyle.Render("←/→")+descStyle.Render("       - Navigate commits"),
		"",
		headerStyle.Render("Other:"),
		keyStyle.Render("?")+descStyle.Render("         - Show this help screen"),
		keyStyle.Render("l")+descStyle.Render("         - View error log"),
		keyStyle.Render("ctrl+h")+descStyle.Render("    - View pane history"),
		keyStyle.Render("K")+descStyle.Render("         - Edit keyboard shortcuts"),
		keyStyle.Render("q")+descStyle.Render("         - Quit the application"),
		keyStyle.Render("mouse")+descStyle.Render("     - Use mouse wheel to scroll"),
	)
	return content
}

func (h helpTypeInstanceStart) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Instance Created"),
		"",
		descStyle.Render("New session created:"),
		descStyle.Render(fmt.Sprintf("• Git branch: %s (isolated worktree)",
			lipgloss.NewStyle().Bold(true).Render(h.instance.Branch))),
		descStyle.Render(fmt.Sprintf("• %s running in background tmux session",
			lipgloss.NewStyle().Bold(true).Render(h.instance.Program))),
		"",
		headerStyle.Render("Managing:"),
		keyStyle.Render("↵/o")+descStyle.Render("   - Attach to the session to interact with it directly"),
		keyStyle.Render("tab")+descStyle.Render("   - Switch between AI, diff, and terminal tabs"),
		keyStyle.Render("D")+descStyle.Render("     - Kill (delete) the selected session"),
		keyStyle.Render("a / P")+descStyle.Render("     - Change program for the selected session (both 'a' and 'P')"),
		keyStyle.Render("l")+descStyle.Render("     - List available/current programs"),
		keyStyle.Render("w")+descStyle.Render("     - Open in IDE"),
		"",
		headerStyle.Render("Git & Handoff:"),
		keyStyle.Render("c")+descStyle.Render("     - Checkout this instance's branch"),
		keyStyle.Render("p")+descStyle.Render("     - Commit and push branch to GitHub"),
		keyStyle.Render("b")+descStyle.Render("     - Rebase with main branch"),
		keyStyle.Render("h")+descStyle.Render("     - Git reset --hard to origin/branch"),
		keyStyle.Render("B")+descStyle.Render("     - Create bookmark commit"),
		keyStyle.Render("g")+descStyle.Render("     - Show git status"),
		"",
		headerStyle.Render("Tools:"),
		keyStyle.Render("t")+descStyle.Render("     - Run tests"),
		keyStyle.Render("l")+descStyle.Render("     - View error log"),
		keyStyle.Render("?")+descStyle.Render("     - Show help"),
	)
	return content
}

func (h helpTypeInstanceAttach) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Attaching to Instance"),
		"",
		descStyle.Render("You are now attached to the tmux session."),
		"",
		headerStyle.Render("Session Control:"),
		keyStyle.Render("ctrl-q")+descStyle.Render("    - Detach from session"),
		keyStyle.Render("ctrl-r")+descStyle.Render("    - Reload the session"),
		"",
		dimStyle.Render("Note: When attached, you're directly interacting with the"),
		dimStyle.Render("Claude Code session. Use the detach command to return"),
		dimStyle.Render("to AI Squad's interface."),
	)
	return content
}

func (h helpTypeInstanceCheckout) toContent() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Checkout Instance"),
		"",
		"Changes will be committed locally. The branch name has been copied to your clipboard.",
		"",
		"You can now checkout the branch in your main repository to review or modify the changes.",
		"When resuming, the session will continue from where you left off.",
		"",
		headerStyle.Render("Available Actions:"),
		keyStyle.Render("c")+descStyle.Render(" - Checkout: commit changes and pause session"),
		keyStyle.Render("r")+descStyle.Render(" - Resume a paused session"),
		keyStyle.Render("p")+descStyle.Render(" - Commit and push branch to GitHub"),
		keyStyle.Render("b")+descStyle.Render(" - Rebase with main branch"),
		keyStyle.Render("h")+descStyle.Render(" - Git reset --hard to origin/branch"),
		"",
		dimStyle.Render("Note: The session is paused after checkout. Use 'r' to resume"),
		dimStyle.Render("when you're ready to continue working with Claude Code."),
	)
	return content
}
func (h helpTypeGeneral) mask() uint32 {
	return 1
}

func (h helpTypeInstanceStart) mask() uint32 {
	return 1 << 1
}
func (h helpTypeInstanceAttach) mask() uint32 {
	return 1 << 2
}
func (h helpTypeInstanceCheckout) mask() uint32 {
	return 1 << 3
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#7D56F4"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#36CFC9"))
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFCC00"))
	descStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
)

// showHelpScreen displays the help screen overlay if it hasn't been shown before
func (m *home) showHelpScreen(helpType helpText, onDismiss func()) (tea.Model, tea.Cmd) {
	// Get the flag for this help type
	var alwaysShow bool
	switch helpType.(type) {
	case helpTypeGeneral:
		alwaysShow = true
	}

	flag := helpType.mask()

	// Check if this help screen has been seen before
	// Only show if we're showing the general help screen or the corresponding flag is not set
	// in the seen bitmask.
	if alwaysShow || (m.appState.GetHelpScreensSeen()&flag) == 0 {
		// Mark this help screen as seen and save state
		if err := m.appState.SetHelpScreensSeen(m.appState.GetHelpScreensSeen() | flag); err != nil {
			log.WarningLog.Printf("Failed to save help screen state: %v", err)
		}

		content := helpType.toContent()

		m.textOverlay = overlay.NewTextOverlay(content)
		m.textOverlay.OnDismiss = onDismiss
		// Set the overlay size based on current window dimensions
		if m.windowWidth > 0 && m.windowHeight > 0 {
			width, height := m.calculateOverlayDimensions()
			m.textOverlay.SetSize(width, height)
		}
		m.state = stateHelp
		return m, nil
	}

	// Skip displaying the help screen
	if onDismiss != nil {
		onDismiss()
	}
	return m, nil
}

// handleHelpState handles key events when in help state
func (m *home) handleHelpState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key press will close the help overlay
	shouldClose := m.textOverlay.HandleKeyPress(msg)
	if shouldClose {
		m.state = stateDefault
		m.textOverlay = nil
		return m, tea.Sequence(
			tea.WindowSize(),
			func() tea.Msg {
				m.menu.SetState(ui.StateDefault)
				return nil
			},
		)
	}

	return m, nil
}
