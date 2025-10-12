package app

import (
	"ai-squad/config"
	"ai-squad/log"
	"ai-squad/ui"
	"ai-squad/ui/overlay"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the application UI based on current state
func (m *home) View() string {
	var listAndPreview string

	if m.currentLayoutMode == config.LayoutModeMobile {
		// Mobile: Vertical stacking without padding
		listAndPreview = lipgloss.JoinVertical(
			lipgloss.Left,
			m.list.String(),
			m.tabbedWindow.String(),
		)
	} else {
		// Full: Horizontal layout with padding
		listWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.list.String())
		previewWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.tabbedWindow.String())
		listAndPreview = lipgloss.JoinHorizontal(lipgloss.Top, listWithPadding, previewWithPadding)
	}

	// Add rebase loading indicator if rebase is in progress
	var rebaseIndicator string
	if m.rebaseInProgress {
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(1, 2)
		rebaseIndicator = loadingStyle.Render(fmt.Sprintf("%s Rebase in progress for branch %s... Waiting for remote update",
			m.spinner.View(), m.rebaseBranchName))
	}

	mainView := lipgloss.JoinVertical(
		lipgloss.Center,
		rebaseIndicator,
		listAndPreview,
		m.menu.String(),
		m.errBox.String(),
	)

	// If an instance is currently being started asynchronously, show a transient centered
	// overlay with a spinner so the user knows work is in progress.
	if m.startingSpinner != nil {
		content := fmt.Sprintf("%s Starting instance...", m.startingSpinner.View())
		return overlay.PlaceOverlay(0, 0, content, mainView, true, true)
	}

	if m.state == statePrompt {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
	} else if m.state == stateHelp {
		if m.textOverlay == nil {
			log.ErrorLog.Printf("text overlay is nil")
			// Return to default state if overlay is nil
			m.state = stateDefault
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
			// Return to default state if overlay is nil
			m.state = stateDefault
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	} else if m.state == stateChangeProgram {
		if m.programListOverlay == nil {
			log.ErrorLog.Printf("program list overlay is nil")
		}
		return overlay.PlaceOverlay(0, 0, m.programListOverlay.Render(), mainView, true, true)
	} else if m.state == stateSelectProgram {
		programs := []string{
			"claude",
			"codex",
			"gemini",
			"qwen",
			"crush",
		}
		var lines []string
		lines = append(lines, "Select program:")
		for i, prog := range programs {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, prog))
		}
		lines = append(lines, "", "Press number to select, Esc to cancel")
		prompt := strings.Join(lines, "\n")
		return lipgloss.Place(80, 3, lipgloss.Center, lipgloss.Center, prompt)
	} else if m.textOverlay != nil {
		return overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateBranchSelect {
		if m.branchSelectorOverlay == nil {
			log.ErrorLog.Printf("branch selector overlay is nil")
			// Return to default state if overlay is nil
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.branchSelectorOverlay.View(), mainView, true, true)
	} else if m.state == stateErrorLog {
		if m.textOverlay == nil {
			log.ErrorLog.Printf("error log overlay is nil")
			m.state = stateDefault
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == statePRReview {
		if m.prReviewOverlay == nil {
			log.ErrorLog.Printf("PR review overlay is nil")
			m.state = stateDefault
			return mainView
		}
		// Return PR review directly - it manages its own full-screen layout
		return m.prReviewOverlay.View()
	} else if m.state == stateBookmark {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
			m.state = stateDefault
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
	} else if m.state == stateHistory {
		if m.historyOverlay == nil {
			log.ErrorLog.Printf("history overlay is nil")
			m.state = stateDefault
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.historyOverlay.Render(), mainView, true, true)
	} else if m.state == stateKeybindingEditor {
		if m.keybindingEditorOverlay == nil {
			log.ErrorLog.Printf("keybinding editor overlay is nil")
			m.state = stateDefault
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.keybindingEditorOverlay.Render(), mainView, true, true)
	} else if m.state == stateGitStatus {
		if m.gitStatusOverlay == nil {
			log.ErrorLog.Printf("git status overlay is nil")
			m.state = stateDefault
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.gitStatusOverlay.Render(), mainView, true, true)
	} else if m.state == stateCommentDetail {
		if m.commentDetailOverlay == nil {
			log.ErrorLog.Printf("comment detail overlay is nil")
			m.state = statePRReview
			return mainView
		}
		return overlay.PlaceOverlay(0, 0, m.commentDetailOverlay.Render(), mainView, true, true)
	}

	return mainView
}
