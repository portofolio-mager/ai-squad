package app

import (
	"ai-squad/keys"
	"ai-squad/log"
	"ai-squad/ui"
	"ai-squad/ui/overlay"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// State handlers for overlays

func (m *home) handleErrorLogState(_ tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key press closes the error log
	m.state = stateDefault
	m.textOverlay = nil
	return m, nil
}

func (m *home) handleHistoryState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Let the history overlay handle the key press
	shouldClose := m.historyOverlay.HandleKeyPress(msg)
	if shouldClose {
		m.state = stateDefault
		m.menu.SetState(ui.StateDefault)
		m.historyOverlay = nil
		return m, tea.WindowSize()
	}

	// Update the viewport
	_, cmd := m.historyOverlay.Update(msg)
	return m, cmd
}

func (m *home) handleCommentDetailState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.commentDetailOverlay == nil {
		m.state = statePRReview
		return m, nil
	}

	// Let the comment detail overlay handle the key press
	shouldClose := m.commentDetailOverlay.HandleKeyPress(msg)
	if shouldClose {
		m.state = statePRReview
		m.commentDetailOverlay = nil
		return m, nil
	}

	// Update the viewport
	_, cmd := m.commentDetailOverlay.Update(msg)
	return m, cmd
}

func (m *home) handleKeybindingEditorState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keybindingEditorOverlay == nil {
		m.state = stateDefault
		return m, nil
	}

	// Let the overlay handle the key press
	if m.keybindingEditorOverlay.HandleKeyPress(msg) {
		// Overlay was dismissed, reload keybindings
		m.state = stateDefault
		m.keybindingEditorOverlay = nil

		// Reload keybindings
		if err := keys.InitializeCustomKeyBindings(); err != nil {
			log.ErrorLog.Printf("Failed to reload custom keybindings: %v", err)
		}

		// Update menu to reflect new keybindings
		m.menu = ui.NewMenu()

		return m, nil
	}

	return m, nil
}

func (m *home) handleGitStatusState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.gitStatusOverlay == nil {
		m.state = stateDefault
		return m, nil
	}

	// Let the overlay handle the key press
	if m.gitStatusOverlay.HandleKeyPress(msg) {
		// Overlay was dismissed, and the OnDismiss callback has already cleaned up.
		return m, nil
	}

	return m, nil
}

// Overlay display functions

func (m *home) showErrorLog() (tea.Model, tea.Cmd) {
	// Create content for error log
	var content string
	if len(m.errorLog) == 0 {
		content = "No errors have been logged."
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Error Log"),
			"",
			"Recent errors (newest first):",
			"")

		// Show errors in reverse order (newest first)
		for i := len(m.errorLog) - 1; i >= 0; i-- {
			content = lipgloss.JoinVertical(lipgloss.Left,
				content,
				m.errorLog[i])
		}

		content = lipgloss.JoinVertical(lipgloss.Left,
			content,
			"",
			dimStyle.Render("Press any key to close"))
	}

	// Create text overlay
	m.textOverlay = overlay.NewTextOverlay(content)
	m.state = stateErrorLog
	m.menu.SetState(ui.StateDefault)

	return m, nil
}

func (m *home) showHistoryView() tea.Cmd {
	selected := m.list.GetSelectedInstance()
	if selected == nil {
		return nil
	}

	var content string
	var title string
	var err error

	// Determine which pane's history to show based on the active tab
	if m.tabbedWindow.IsInTerminalTab() {
		// Show terminal pane history
		content, err = selected.GetTerminalFullHistory()
		if err != nil {
			return m.handleError(fmt.Errorf("failed to get terminal history: %v", err))
		}
		title = fmt.Sprintf("Terminal History - %s", selected.Title)
	} else if m.tabbedWindow.IsInAITab() {
		// Show AI pane history
		content, err = selected.GetAIFullHistory()
		if err != nil {
			return m.handleError(fmt.Errorf("failed to get AI history: %v", err))
		}
		title = fmt.Sprintf("AI History - %s", selected.Title)
	} else {
		// Default to AI pane if we're in diff view
		content, err = selected.GetAIFullHistory()
		if err != nil {
			return m.handleError(fmt.Errorf("failed to get AI history: %v", err))
		}
		title = fmt.Sprintf("AI History - %s", selected.Title)
	}

	// Create the history overlay
	m.historyOverlay = overlay.NewHistoryOverlay(title, content)
	m.historyOverlay.OnDismiss = func() {
		m.state = stateDefault
		m.menu.SetState(ui.StateDefault)
		m.historyOverlay = nil
	}

	// Set state to history
	m.state = stateHistory
	m.menu.SetState(ui.StateDefault)

	return tea.WindowSize()
}
