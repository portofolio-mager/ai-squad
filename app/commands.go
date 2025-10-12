package app

import (
	"ai-squad/keys"
	"ai-squad/log"
	"ai-squad/session"
	"ai-squad/ui/overlay"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickUpdateMetadataCmd is the callback to update the metadata of the instances every 500ms. Note that we iterate
// overall the instances and capture their output. It's a pretty expensive operation. Let's do it 2x a second only.
var tickUpdateMetadataCmd = func() tea.Msg {
	time.Sleep(500 * time.Millisecond)
	return tickUpdateMetadataMessage{}
}

// startInstanceCmd runs instance.Start(true) off the UI goroutine and returns an instanceStartResultMsg.
func startInstanceCmd(instance *session.Instance, index int, promptAfter bool) tea.Cmd {
	return func() tea.Msg {
		err := instance.Start(true)
		return instanceStartResultMsg{
			Index:       index,
			Err:         err,
			PromptAfter: promptAfter,
		}
	}
}

// keydownCallback clears the menu option highlighting after 500ms.
func (m *home) keydownCallback(name keys.KeyName) tea.Cmd {
	m.menu.Keydown(name)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}

		return keyupMsg{}
	}
}

// handleError handles all errors which get bubbled up to the app. sets the error message. We return a callback tea.Cmd that returns a hideErrMsg message
// which clears the error message after 3 seconds.
func (m *home) handleError(err error) tea.Cmd {
	log.ErrorLog.Printf("%v", err)
	m.errBox.SetError(err)

	// Store error in the error log with timestamp
	timestamp := time.Now().Format("15:04:05")
	errorMsg := fmt.Sprintf("[%s] %v", timestamp, err)
	m.errorLog = append(m.errorLog, errorMsg)

	// Keep only the last 100 errors to prevent memory issues
	if len(m.errorLog) > 100 {
		m.errorLog = m.errorLog[len(m.errorLog)-100:]
	}

	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(3 * time.Second):
		}

		return hideErrMsg{}
	}
}

// createRemotePollingCmd creates a command that polls the remote for branch changes
func (m *home) createRemotePollingCmd(branchName string, originalSHA string) tea.Cmd {
	return func() tea.Msg {
		// Wait a bit before polling
		time.Sleep(3 * time.Second)

		return remotePollingMsg{
			branchName:  branchName,
			originalSHA: originalSHA,
		}
	}
}

// confirmAction shows a confirmation modal and stores the action to execute on confirm
func (m *home) confirmAction(message string, action tea.Cmd) tea.Cmd {
	m.state = stateConfirm

	// Create and show the confirmation overlay using ConfirmationOverlay
	m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	// Set a fixed width for consistent appearance
	m.confirmationOverlay.SetWidth(50)

	// Store the pending command
	m.pendingCmd = action

	// Set callbacks for confirmation and cancellation
	m.confirmationOverlay.OnConfirm = func() {
		m.state = stateDefault
	}

	m.confirmationOverlay.OnCancel = func() {
		m.state = stateDefault
		m.pendingCmd = nil
	}

	return nil
}

// calculateOverlayDimensions returns the width and height for overlay components
func (m *home) calculateOverlayDimensions() (width, height int) {
	width = int(float32(m.windowWidth) * overlayWidthRatio)
	height = int(float32(m.windowHeight) * overlayHeightRatio)
	return width, height
}
