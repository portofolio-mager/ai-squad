package app

import (
	"ai-squad/config"
	"ai-squad/keys"
	"ai-squad/log"
	"ai-squad/session"
	"ai-squad/session/git"
	"ai-squad/ui"
	"ai-squad/ui/overlay"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Run is the main entrypoint into the application.
func Run(ctx context.Context, program string, autoYes bool) error {
	p := tea.NewProgram(
		newHome(ctx, program, autoYes),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // Mouse scroll
	)
	_, err := p.Run()
	return err
}

func newHome(ctx context.Context, program string, autoYes bool) *home {
	// Load application config
	appConfig := config.LoadConfig()

	// Load application state
	appState := config.LoadState()

	// Initialize custom keybindings
	if err := keys.InitializeCustomKeyBindings(); err != nil {
		// Log error but continue with defaults
		log.ErrorLog.Printf("Failed to load custom keybindings: %v", err)
	}

	// Initialize storage
	storage, err := session.NewStorage(appState)
	if err != nil {
		fmt.Printf("Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	// Resolve the program to use on startup. If the caller provided a program, prefer it;
	// otherwise use the configured default. ResolveProgramCommand will attempt to honor shell
	// aliases and PATH to return a usable executable path plus original args.
	startProgram := program
	if strings.TrimSpace(startProgram) == "" {
		startProgram = appConfig.DefaultProgram
	}
	startProgram = config.ResolveProgramCommand(startProgram)

	// Create update checker
	updateChecker := NewUpdateChecker()
	updateChecker.StartBackgroundCheck()

	menu := ui.NewMenu()
	menu.SetUpdateChecker(updateChecker)

	h := &home{
		ctx:           ctx,
		spinner:       spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:          menu,
		tabbedWindow:  ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane(), ui.NewJestPane(appConfig)),
		errBox:        ui.NewErrBox(),
		storage:       storage,
		appConfig:     appConfig,
		program:       startProgram,
		autoYes:       autoYes,
		state:         stateDefault,
		appState:      appState,
		updateChecker: updateChecker,
	}
	h.list = ui.NewList(&h.spinner, autoYes)

	// Load saved instances
	instances, err := storage.LoadInstances()
	if err != nil {
		fmt.Printf("Failed to load instances: %v\n", err)
		os.Exit(1)
	}

	// Add loaded instances to the list
	for _, instance := range instances {
		// Call the finalizer immediately.
		h.list.AddInstance(instance)()
		if autoYes {
			instance.AutoYes = true
		}
	}

	return h
}

func (m *home) Init() tea.Cmd {
	// Upon starting, we want to start the spinner. Whenever we get a spinner.TickMsg, we
	// update the spinner, which sends a new spinner.TickMsg. I think this lasts forever lol.
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return previewTickMsg{}
		},
		tickUpdateMetadataCmd,
		// Force an initial window size event to ensure proper layout initialization
		tea.WindowSize(),
	)
}

func (m *home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle branch selector updates when in that state
	if m.state == stateBranchSelect && m.branchSelectorOverlay != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			// Update the branch selector
			_, cmd := m.branchSelectorOverlay.Update(msg)

			// Check if selection is complete
			if m.branchSelectorOverlay.IsSelected() {
				selectedBranch := m.branchSelectorOverlay.SelectedBranch()
				if selectedBranch == "" {
					// User cancelled
					m.state = stateDefault
					m.menu.SetState(ui.StateDefault)
					m.branchSelectorOverlay = nil
					return m, nil
				}

				// Create instance with selected branch
				return m.createInstanceWithBranch(selectedBranch)
			}

			return m, cmd
		}
	}

	// Handle PR review updates when in that state
	if m.state == statePRReview && m.prReviewOverlay != nil {
		// Always pass window size messages to ensure the overlay initializes
		if _, ok := msg.(tea.WindowSizeMsg); ok {
			updatedModel, cmd := m.prReviewOverlay.Update(msg)
			*m.prReviewOverlay = updatedModel
			return m, cmd
		}

		updatedModel, cmd := m.prReviewOverlay.Update(msg)
		*m.prReviewOverlay = updatedModel

		// Check for completion or cancellation messages
		switch msg := msg.(type) {
		case ui.PRReviewCompleteMsg:
			// Handle accepted comments
			acceptedComments := msg.AcceptedComments
			m.state = stateDefault
			m.prReviewOverlay = nil

			// Process accepted comments with Claude
			if len(acceptedComments) > 0 {
				return m, m.processAcceptedComments(acceptedComments)
			}
			return m, nil
		case ui.PRReviewCancelMsg:
			// User cancelled
			m.state = stateDefault
			m.prReviewOverlay = nil
			return m, nil
		case ui.PRReviewShowCommentMsg:
			// Show comment detail overlay
			showMsg := msg
			m.commentDetailOverlay = overlay.NewCommentDetailOverlay(showMsg.Comment)
			// We'll set the size in the next WindowSizeMsg
			m.state = stateCommentDetail
			return m, tea.WindowSize()
		case ui.PRRequestResolveConfirmationMsg:
			return m.requestResolveAllConversationsConfirmation()
		}

		return m, cmd
	}

	switch msg := msg.(type) {
	case hideErrMsg:
		m.errBox.Clear()
	case previewTickMsg:
		cmd := m.instanceChanged()
		return m, tea.Batch(
			cmd,
			func() tea.Msg {
				time.Sleep(100 * time.Millisecond)
				return previewTickMsg{}
			},
		)
	case keyupMsg:
		m.menu.ClearKeydown()
		return m, nil
	case tickUpdateMetadataMessage:
		for _, instance := range m.list.GetInstances() {
			if !instance.Started() || instance.Paused() {
				continue
			}
			updated, prompt := instance.HasUpdated()
			if updated {
				instance.SetStatus(session.Running)
			} else {
				if prompt {
					instance.TapEnter()
				} else {
					instance.SetStatus(session.Ready)
				}
			}
			if err := instance.UpdateDiffStats(); err != nil {
				log.WarningLog.Printf("could not update diff stats: %v", err)
			}
		}
		return m, tickUpdateMetadataCmd
	case tea.MouseMsg:
		// Handle mouse wheel events for scrolling the diff/preview pane
		if msg.Action == tea.MouseActionPress {
			if msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelUp {
				selected := m.list.GetSelectedInstance()
				if selected == nil || selected.Status == session.Paused {
					return m, nil
				}

				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.tabbedWindow.ScrollUp()
				case tea.MouseButtonWheelDown:
					m.tabbedWindow.ScrollDown()
				}
				return m, nil
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.updateHandleWindowSizeEvent(msg)

		// Also update PR review overlay if it's active
		if m.state == statePRReview && m.prReviewOverlay != nil {
			updatedModel, _ := m.prReviewOverlay.Update(msg)
			*m.prReviewOverlay = updatedModel
		}

		// Also update comment detail overlay if it's active
		if m.state == stateCommentDetail && m.commentDetailOverlay != nil {
			m.commentDetailOverlay.SetSize(msg.Width, msg.Height)
		}

		return m, nil
	case error:
		// Handle errors from confirmation actions
		return m, m.handleError(msg)
	case instanceChangedMsg:
		// Handle instance changed after confirmation action
		return m, m.instanceChanged()
	case instanceStartResultMsg:
		// Stop the transient starting spinner as the async start finished.
		m.startingSpinner = nil

		// Handle asynchronous instance start completion.
		if msg.Err != nil {
			// Ensure we select the instance that failed (in case selection moved).
			m.list.SetSelectedInstance(msg.Index)
			// Remove the instance and surface the error.
			m.list.Kill()
			m.state = stateDefault
			return m, m.handleError(msg.Err)
		}
		// Success: finalize the instance, persist state, and optionally open prompt/help.
		if m.newInstanceFinalizer != nil {
			m.newInstanceFinalizer()
		}
		// Set autoyes if global flag enabled.
		if m.autoYes {
			if inst := m.list.GetInstances()[msg.Index]; inst != nil {
				inst.AutoYes = true
			}
		}
		// Save after adding new instance
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}
		m.state = stateDefault
		if msg.PromptAfter {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = overlay.NewTextInputOverlay("Enter prompt", "")
		} else {
			m.menu.SetState(ui.StateDefault)
			if inst := m.list.GetInstances()[msg.Index]; inst != nil {
				m.showHelpScreen(helpStart(inst), nil)
			}
		}
		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case instanceCreatedMsg:
		// Handle instance creation completion
		if msg.err != nil {
			// Remove the instance on error
			m.list.Kill()
			return m, m.handleError(msg.err)
		}
		// Show help screen on successful creation
		m.showHelpScreen(helpStart(msg.instance), nil)
		return m, m.instanceChanged()
	case instanceDeletedMsg:
		// Handle instance deletion completion
		if msg.err != nil {
			return m, m.handleError(msg.err)
		}
		return m, m.instanceChanged()
	case startRebaseMsg:
		// Handle the actual rebase after confirmation
		if m.pendingRebaseInstance == nil {
			return m, nil
		}

		// Clear the pending instance
		instance := m.pendingRebaseInstance
		m.pendingRebaseInstance = nil

		// Execute rebase synchronously here to handle the result immediately
		worktree, err := instance.GetGitWorktree()
		if err != nil {
			return m, m.handleError(err)
		}

		// Check if there are uncommitted changes
		isDirty, err := worktree.IsDirty()
		if err != nil {
			return m, m.handleError(err)
		}

		if isDirty {
			return m, m.handleError(errors.New(cannotRebaseUncommittedChangesError))
		}

		// Get current commit SHA before rebase
		currentSHA, err := worktree.GetCurrentCommitSHA()
		if err != nil {
			return m, m.handleError(fmt.Errorf("failed to get current commit: %w", err))
		}

		// Perform the rebase
		if err := worktree.RebaseWithMain(); err != nil {
			// Check if this is a rebase conflict error that needs polling
			if rebaseErr, ok := err.(*git.RebaseConflictError); ok {
				log.InfoLog.Printf("Rebase conflict detected for branch %s", worktree.GetBranchName())

				// Display the error with instructions
				errorCmd := m.handleError(fmt.Errorf("rebase conflicts detected. editor opened at %s\nresolve conflicts, complete rebase, and push to remote", rebaseErr.TempDir))

				// Set rebase in progress state
				m.rebaseInProgress = true
				m.rebaseInstance = instance
				m.rebaseBranchName = worktree.GetBranchName()
				m.rebaseOriginalSHA = currentSHA

				// Start polling the remote for changes
				pollingCmd := m.createRemotePollingCmd(worktree.GetBranchName(), currentSHA)

				// Return both commands so error displays AND polling starts
				return m, tea.Batch(errorCmd, pollingCmd)
			}
			return m, m.handleError(err)
		}

		// Success
		return m, m.instanceChanged()
	case startGitResetMsg:
		// Handle the actual git reset after confirmation
		if m.pendingResetInstance == nil {
			return m, nil
		}

		// Clear the pending instance
		instance := m.pendingResetInstance
		m.pendingResetInstance = nil

		// Execute reset synchronously here to handle the result immediately
		worktree, err := instance.GetGitWorktree()
		if err != nil {
			return m, m.handleError(err)
		}

		// Get branch name before reset
		branchName := worktree.GetBranchName()

		// Perform the reset
		if err := worktree.ResetToOrigin(); err != nil {
			return m, m.handleError(err)
		}

		// Show success message in the status bar
		successMsg := fmt.Sprintf("✓ Git reset for branch %s completed successfully", branchName)
		m.errBox.SetError(errors.New(successMsg))

		// Also add to log for history
		timestamp := time.Now().Format("15:04:05")
		m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] %s", timestamp, successMsg))

		// Refresh instances and hide message after a delay
		return m, tea.Batch(
			m.instanceChanged(),
			func() tea.Msg {
				time.Sleep(3 * time.Second)
				return hideErrMsg{}
			},
		)

	case remotePollingMsg:
		// Check if rebase is still in progress
		if !m.rebaseInProgress || m.rebaseInstance == nil {
			return m, nil
		}

		// Get the worktree to check remote
		worktree, err := m.rebaseInstance.GetGitWorktree()
		if err != nil {
			log.ErrorLog.Printf("Failed to get worktree for polling: %v", err)
			return m, m.createRemotePollingCmd(msg.branchName, msg.originalSHA)
		}

		// Fetch latest from remote
		if _, err := worktree.FetchBranch(msg.branchName); err != nil {
			log.WarningLog.Printf("Failed to fetch branch %s: %v", msg.branchName, err)
			// Continue polling even if fetch fails
			return m, m.createRemotePollingCmd(msg.branchName, msg.originalSHA)
		}

		// Check if remote SHA has changed
		remoteSHA, err := worktree.GetRemoteBranchSHA(msg.branchName)
		if err != nil {
			log.ErrorLog.Printf("Failed to get remote SHA: %v", err)
			return m, m.createRemotePollingCmd(msg.branchName, msg.originalSHA)
		}

		log.InfoLog.Printf("Polling rebase: original=%s, remote=%s", msg.originalSHA, remoteSHA)

		if remoteSHA != msg.originalSHA {
			// Remote has changed, pull the changes
			log.InfoLog.Printf("Remote branch updated, pulling changes")

			// Reset to the remote branch
			if err := worktree.ResetToRemote(msg.branchName); err != nil {
				m.rebaseInProgress = false
				return m, m.handleError(fmt.Errorf("failed to sync rebased changes: %w", err))
			}

			// Clear rebase state
			m.rebaseInProgress = false
			m.rebaseInstance = nil
			m.rebaseBranchName = ""
			m.rebaseOriginalSHA = ""

			// Show success
			timestamp := time.Now().Format("15:04:05")
			m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] Rebase completed successfully", timestamp))

			return m, m.instanceChanged()
		}

		// Continue polling
		return m, m.createRemotePollingCmd(msg.branchName, msg.originalSHA)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// If a transient starting spinner exists, update it as well so it animates while
		// the instance start is in progress.
		if m.startingSpinner != nil {
			var cmd2 tea.Cmd
			*m.startingSpinner, cmd2 = (*m.startingSpinner).Update(msg)
			return m, tea.Batch(cmd, cmd2)
		}
		return m, cmd
	case allCommentsProcessedMsg:
		// Comments have been processed, return to default state
		m.state = stateDefault
		m.textOverlay = nil

		// Show success message
		// Note: Using error box for now to show success message
		successErr := errors.New("pr comments processed successfully")
		m.errBox.SetError(successErr)
		return m, func() tea.Msg {
			time.Sleep(3 * time.Second)
			return hideErrMsg{}
		}
	case resolveConversationsMsg:
		// Show result of resolving conversations
		m.state = stateDefault
		m.textOverlay = nil

		// Add all logs from the operation
		if len(msg.logs) > 0 {
			m.errorLog = append(m.errorLog, msg.logs...)
		}

		timestamp := time.Now().Format("15:04:05")

		if msg.err != nil {
			// Log the error
			m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] Failed to resolve conversations: %v", timestamp, msg.err))
			m.errBox.SetError(msg.err)
		} else {
			// Show success message
			var message string
			if msg.total == 0 {
				message = "✓ No unresolved review threads found"
				m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] No unresolved review threads found on PR", timestamp))
			} else if msg.resolved == msg.total {
				message = fmt.Sprintf("✓ Successfully resolved all %d review threads", msg.total)
				m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] Successfully resolved all %d review threads", timestamp, msg.total))
			} else {
				message = fmt.Sprintf("✓ Resolved %d of %d review threads", msg.resolved, msg.total)
				m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] Resolved %d of %d review threads (some failed)", timestamp, msg.resolved, msg.total))
			}

			successErr := errors.New(message)
			m.errBox.SetError(successErr)
		}

		// Keep log size manageable
		if len(m.errorLog) > 100 {
			m.errorLog = m.errorLog[len(m.errorLog)-100:]
		}

		// Return command to hide error after delay only if no error
		if msg.err == nil {
			return m, func() tea.Msg {
				time.Sleep(3 * time.Second)
				return hideErrMsg{}
			}
		}
		return m, nil
	case ui.PRResolveAllConversationsMsg:
		// Resolve all conversations on the PR
		m.state = stateHelp
		m.prReviewOverlay = nil
		m.confirmationOverlay = nil
		m.textOverlay = overlay.NewTextOverlay("Resolving all PR conversations...\n\nThis may take a moment...")

		// Log the start of resolution
		timestamp := time.Now().Format("15:04:05")
		m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] Starting to resolve all PR conversations...", timestamp))

		return m, m.resolveAllPRConversations()
	case testStartedMsg:
		// Show non-obtrusive message that tests are running
		m.errBox.SetError(errors.New("running jest tests"))
		return m, nil
	case testProgressMsg:
		// Update test progress
		var status string
		if msg.running {
			status = fmt.Sprintf("Running tests: %d/%d passed, %d failed", msg.passed, msg.total, msg.failed)
		} else {
			status = fmt.Sprintf("Tests complete: %d/%d passed, %d failed", msg.passed, msg.total, msg.failed)
		}
		m.errBox.SetError(errors.New(status))
		return m, nil
	case testResultsMsg:
		// Handle test results
		if msg.err != nil {
			return m, m.handleError(msg.err)
		}

		// Parse final stats from output
		finalStats := parseJestFinalStats(msg.output)

		// Open failed test files in IDE if any
		if len(msg.failedFiles) > 0 {
			// Get the IDE command from configuration - use the first failed file's directory for context
			globalConfig := m.appConfig
			fileDir := filepath.Dir(msg.failedFiles[0])
			ideCommand := config.GetEffectiveIdeCommand(fileDir, globalConfig)

			for _, file := range msg.failedFiles {
				cmd := exec.Command(ideCommand, file)
				cmd.Start()
			}
			// Show brief status about failed tests with counts
			m.errBox.SetError(fmt.Errorf("tests completed: %d/%d passed, %d failed. Opening failed files in editor (%s)",
				finalStats.passed, finalStats.total, finalStats.failed, ideCommand))
		} else {
			// All tests passed
			m.errBox.SetError(fmt.Errorf("all tests passed! %d/%d test suites completed",
				finalStats.passed, finalStats.total))
		}

		// Auto-hide the message after 5 seconds (give more time to read the stats)
		return m, func() tea.Msg {
			time.Sleep(5 * time.Second)
			return hideErrMsg{}
		}
	}
	return m, nil
}

func (m *home) handleQuit() (tea.Model, tea.Cmd) {
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, m.handleError(err)
	}
	return m, tea.Quit
}

func (m *home) handleMenuHighlighting(msg tea.KeyMsg) (cmd tea.Cmd, returnEarly bool) {
	// Handle menu highlighting when you press a button. We intercept it here and immediately return to
	// update the ui while re-sending the keypress. Then, on the next call to this, we actually handle the keypress.
	if m.keySent {
		m.keySent = false
		return nil, false
	}
	// Don't intercept keys when in any overlay/input-like states (prompt, help, confirmation, change program, select program).
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm || m.state == stateChangeProgram || m.state == stateSelectProgram {
		return nil, false
	}
	// If it's in the global keymap, we should try to highlight it.
	name, ok := keys.GetKeyName(msg.String())
	if !ok {
		return nil, false
	}

	if m.list.GetSelectedInstance() != nil && m.list.GetSelectedInstance().Paused() && name == keys.KeyEnter {
		return nil, false
	}
	if name == keys.KeyShiftDown || name == keys.KeyShiftUp {
		return nil, false
	}

	// Skip the menu highlighting if the key is not in the map or we are using the shift up and down keys.
	// TODO: cleanup: when you press enter on stateNew, we use keys.KeySubmitName. We should unify the keymap.
	if name == keys.KeyEnter && m.state == stateNew {
		name = keys.KeySubmitName
	}
	m.keySent = true
	return tea.Batch(
		func() tea.Msg { return msg },
		m.keydownCallback(name)), true
}

func (m *home) handleKeyPress(msg tea.KeyMsg) (mod tea.Model, cmd tea.Cmd) {
	cmd, returnEarly := m.handleMenuHighlighting(msg)
	if returnEarly {
		return m, cmd
	}

	if m.state == stateHelp {
		return m.handleHelpState(msg)
	}

	if m.state == stateErrorLog {
		return m.handleErrorLogState(msg)
	}

	if m.state == stateHistory {
		return m.handleHistoryState(msg)
	}

	if m.state == stateKeybindingEditor {
		return m.handleKeybindingEditorState(msg)
	}

	if m.state == stateGitStatus {
		return m.handleGitStatusState(msg)
	}

	if m.state == stateCommentDetail {
		return m.handleCommentDetailState(msg)
	}

	switch m.state {
	case stateNew:
		// Handle quit commands first. Don't handle q because the user might want to type that.
		if msg.String() == "ctrl+c" {
			m.state = stateDefault
			m.promptAfterName = false
			m.list.Kill()
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		}

		instance := m.list.GetInstances()[m.list.NumInstances()-1]
		switch msg.Type {
		// Start the instance (enable previews etc) and go back to the main menu state.
		case tea.KeyEnter:
			if len(instance.Title) == 0 {
				return m, m.handleError(fmt.Errorf("title cannot be empty"))
			}

			// Capture the index of the instance and flags needed after startup.
			instanceIndex := m.list.NumInstances() - 1
			promptAfter := m.promptAfterName
			// Move UI back to default state immediately so the UI remains responsive.
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)
			// Reset promptAfterName to avoid double-handling when result arrives.
			m.promptAfterName = false

			// Initialize a transient spinner to show while the instance starts.
			s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
			m.startingSpinner = &s

			// Start the instance asynchronously and run the spinner tick concurrently. The spinner
			// will be updated via spinner.TickMsg in Update until instanceStartResultMsg is received.
			return m, tea.Batch(
				m.startingSpinner.Tick,
				startInstanceCmd(m.list.GetInstances()[instanceIndex], instanceIndex, promptAfter),
			)
		case tea.KeyRunes:
			if len(instance.Title) >= 32 {
				return m, m.handleError(fmt.Errorf("title cannot be longer than 32 characters"))
			}
			if err := instance.SetTitle(instance.Title + string(msg.Runes)); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyBackspace:
			if len(instance.Title) == 0 {
				return m, nil
			}
			if err := instance.SetTitle(instance.Title[:len(instance.Title)-1]); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeySpace:
			if err := instance.SetTitle(instance.Title + " "); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyEsc:
			m.list.Kill()
			m.state = stateDefault
			m.instanceChanged()

			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		default:
		}
		return m, nil
	case statePrompt:
		// Use the new TextInputOverlay component to handle all key events
		shouldClose := m.textInputOverlay.HandleKeyPress(msg)

		// Check if the form was submitted or canceled
		if shouldClose {
			selected := m.list.GetSelectedInstance()
			// TODO: this should never happen since we set the instance in the previous state.
			if selected == nil {
				return m, nil
			}
			if m.textInputOverlay.IsSubmitted() {
				if err := selected.SendPrompt(m.textInputOverlay.GetValue()); err != nil {
					// TODO: we probably end up in a bad state here.
					return m, m.handleError(err)
				}
			}

			// Close the overlay and reset state
			m.textInputOverlay = nil
			m.state = stateDefault
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					m.showHelpScreen(helpStart(selected), nil)
					return nil
				},
			)
		}

		return m, nil
	case stateChangeProgram:
		// Delegate key handling to the programListOverlay
		if m.programListOverlay == nil {
			// If overlay is missing, reset state to default to avoid being stuck
			log.ErrorLog.Printf("program list overlay is nil")
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)
			return m, nil
		}
		shouldClose := m.programListOverlay.HandleKeyPress(msg)
		if shouldClose {
			// If submitted, resolve and set program
			if m.programListOverlay.IsSubmitted() {
				val := m.programListOverlay.GetSelected()
				if len(val) == 0 {
					// Error and close
					m.programListOverlay = nil
					m.state = stateDefault
					return m, m.handleError(fmt.Errorf("program cannot be empty"))
				}
				m.program = config.ResolveProgramCommand(val)
			}
			// Close overlay and reset state
			m.programListOverlay = nil
			m.state = stateDefault
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		}
		return m, nil
	case stateSelectProgram:
		// Handle cancel/escape first
		if msg.String() == "esc" || msg.String() == "ctrl+c" {
			m.state = stateDefault
			return m, nil
		}

		// Support numeric selection for any item in the list (1..n).
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			r := msg.Runes[0]
			if r >= '1' && r <= '9' {
				idx := int(r - '1')
				if idx < len(programs) {
					// Resolve the selected program to honor aliases / PATH for the executable portion.
					m.program = config.ResolveProgramCommand(programs[idx])
				}
				m.state = stateDefault
				return m, nil
			}
		}
	case stateBookmark:
		// Handle bookmark state
		shouldClose := m.textInputOverlay.HandleKeyPress(msg)

		if shouldClose {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}

			var finalCmd tea.Cmd = tea.WindowSize()
			if m.textInputOverlay.IsSubmitted() {
				// Create bookmark commit
				commitMsg := m.textInputOverlay.GetValue()
				cmd := m.createBookmarkCommit(selected, commitMsg)
				finalCmd = tea.Batch(tea.WindowSize(), cmd)
			}

			// Common state reset logic
			m.textInputOverlay = nil
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)

			return m, finalCmd
		}

	}

	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			// Capture confirmation state before clearing overlay
			wasConfirmed := m.confirmationOverlay.IsConfirmed()

			// Check if we should return to PR review state
			returnToPRReview := m.prReviewOverlay != nil

			m.confirmationOverlay = nil

			// Execute pending command if confirmed
			if wasConfirmed && m.pendingCmd != nil {
				cmd := m.pendingCmd
				m.pendingCmd = nil
				// Execute the action and get the result
				result := cmd()
				// If result is a tea.Cmd, return it to be executed
				if resultCmd, ok := result.(tea.Cmd); ok {
					return m, resultCmd
				}
				// Otherwise handle as a message
				return m.Update(result)
			}
			m.pendingCmd = nil

			// Set appropriate state after handling confirmation
			if returnToPRReview {
				m.state = statePRReview
			} else {
				m.state = stateDefault
			}

			return m, nil
		}
		return m, nil
	}

	// Exit scrolling mode when ESC is pressed and preview or terminal pane is in scrolling mode
	// Check if Escape key was pressed
	// Always check for escape key first to ensure it doesn't get intercepted elsewhere
	if msg.Type == tea.KeyEsc {
		// Use the selected instance from the list
		selected := m.list.GetSelectedInstance()

		// If in preview tab and in scroll mode, exit scroll mode
		if !m.tabbedWindow.IsInDiffTab() && !m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsPreviewInScrollMode() {
			err := m.tabbedWindow.ResetPreviewToNormalMode(selected)
			if err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}

		// If in terminal tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsTerminalInScrollMode() {
			err := m.tabbedWindow.ResetTerminalToNormalMode(selected)
			if err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
	}

	// Handle text overlay if present
	if m.textOverlay != nil {
		shouldClose := m.textOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.textOverlay = nil
		}
		return m, nil
	}

	// Handle quit commands first
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m.handleQuit()
	}

	// Handle Jest-specific keybindings when in Jest tab
	if m.tabbedWindow.IsInJestTab() {
		switch msg.String() {
		case "r":
			m.tabbedWindow.JestRerunTests()
			return m, nil
		}
	}

	name, ok := keys.GetKeyName(msg.String())
	if !ok {
		return m, nil
	}

	switch name {
	case keys.KeyHelp:
		return m.showHelpScreen(helpTypeGeneral{}, nil)
	case keys.KeyErrorLog:
		return m.showErrorLog()
	case keys.KeyEditKeybindings:
		m.state = stateKeybindingEditor
		m.keybindingEditorOverlay = overlay.NewKeybindingEditorOverlay()
		return m, nil
	case keys.KeyPrompt:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)
		m.promptAfterName = true

		return m, nil
	case keys.KeyChangeProgram:
		// Open program selection overlay to change the program
		m.state = stateChangeProgram
		m.menu.SetState(ui.StatePrompt)
		// Initialize program list overlay with available programs and preselect current program
		m.programListOverlay = overlay.NewProgramListOverlay(programs, m.program)
		// Request a window size message so the overlay sizing logic runs
		return m, tea.Batch(tea.WindowSize())
	case keys.KeyNew:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)

		return m, nil
	case keys.KeyExistingBranch:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}

		// Show branch selector
		m.state = stateBranchSelect
		m.menu.SetState(ui.StateNewInstance)

		// Get list of remote branches
		branches, err := git.ListRemoteBranchesFromRepo(".")
		if err != nil {
			return m, m.handleError(fmt.Errorf("failed to list remote branches: %w", err))
		}

		// Check if there are any branches
		if len(branches) == 0 {
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)
			return m, m.handleError(fmt.Errorf("no remote branches found"))
		}

		// Create branch selector overlay
		m.branchSelectorOverlay = overlay.NewBranchSelectorOverlay(branches)

		// Initialize the branch selector
		return m, m.branchSelectorOverlay.Init()
	case keys.KeyUp:
		if m.scrollLocked && m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.ScrollUp()
		} else {
			m.list.Up()
		}
		return m, m.instanceChanged()
	case keys.KeyDown:
		if m.scrollLocked && m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.ScrollDown()
		} else {
			m.list.Down()
		}
		return m, m.instanceChanged()
	// case keys.KeyLeft:
	// 	m.list.Left()
	// 	return m, m.instanceChanged()
	// case keys.KeyRight:
	// 	m.list.Right()
	// 	return m, m.instanceChanged()
	case keys.KeyShiftUp:
		m.tabbedWindow.ScrollUp()
		return m, nil
	case keys.KeyShiftDown:
		m.tabbedWindow.ScrollDown()
		return m, nil
	case keys.KeyHome:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.ScrollToTop()
		}
		return m, m.instanceChanged()
	case keys.KeyEnd:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.ScrollToBottom()
		}
		return m, m.instanceChanged()
	case keys.KeyPageUp:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.PageUp()
		}
		return m, m.instanceChanged()
	case keys.KeyPageDown:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.PageDown()
		}
		return m, m.instanceChanged()
	case keys.KeyAltUp:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.JumpToPrevFile()
		}
		return m, m.instanceChanged()
	case keys.KeyAltDown:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.JumpToNextFile()
		}
		return m, m.instanceChanged()
	case keys.KeyTab:
		return m.handleTabSwitch(false)
	case keys.KeyShiftTab:
		return m.handleTabSwitch(true)
	case keys.KeyDiffAll:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.SetDiffModeAll()
		}
		return m, m.instanceChanged()
	case keys.KeyDiffLastCommit:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.SetDiffModeLastCommit()
		}
		return m, m.instanceChanged()
	case keys.KeyLeft:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.NavigateToPrevCommit()
		}
		m.list.Left()
		return m, m.instanceChanged()
	case keys.KeyRight:
		if m.tabbedWindow.IsInDiffTab() {
			m.tabbedWindow.NavigateToNextCommit()
		}
		m.list.Right()
		return m, m.instanceChanged()
	case keys.KeyScrollLock:
		if m.tabbedWindow.IsInDiffTab() {
			m.scrollLocked = !m.scrollLocked
			m.menu.SetScrollLocked(m.scrollLocked)
		}
		return m, nil
	case keys.KeyOpenInIDE:
		// Only handle 'i' when in diff view
		if m.tabbedWindow.IsInDiffTab() {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}
			// Get the current file from diff view
			currentFile := m.tabbedWindow.GetCurrentDiffFile()
			if currentFile == "" {
				return m, m.handleError(fmt.Errorf("no file selected. Navigate to a file in the diff view first"))
			}
			// Open the file in IDE
			cmd := m.openFileInIDE(selected, currentFile)
			return m, cmd
		}
		return m, nil
	case keys.KeyExternalDiff:
		// Only handle 'x' when in diff view
		if m.tabbedWindow.IsInDiffTab() {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}
			// Get the current file from diff view
			currentFile := m.tabbedWindow.GetCurrentDiffFile()
			if currentFile == "" {
				return m, m.handleError(fmt.Errorf("no file selected. Navigate to a file in the diff view first"))
			}
			// Open the file in external diff tool
			cmd := m.openFileInExternalDiff(selected, currentFile)
			return m, cmd
		}
		return m, nil
	case keys.KeyKill:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}

		// Create the kill action as a tea.Cmd
		killAction := func() tea.Msg {
			// Delete from storage first
			if err := m.storage.DeleteInstance(selected.Title); err != nil {
				return err
			}

			// Start async kill and return a command
			// The kill logic will handle checked out branches
			return m.killInstanceAsync(selected)
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
		return m, m.confirmAction(message, killAction)
	case keys.KeySubmit:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}

		// Create the push action as a tea.Cmd
		pushAction := func() tea.Msg {
			// Default commit message with timestamp
			commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s", selected.Title, time.Now().Format(time.RFC822))
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}
			if err = worktree.PushChanges(commitMsg, true); err != nil {
				return err
			}
			return nil
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Push changes from session '%s'?", selected.Title)
		return m, m.confirmAction(message, pushAction)
	case keys.KeyCheckout:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}

		// Show help screen before pausing
		m.showHelpScreen(helpTypeInstanceCheckout{}, func() {
			if err := selected.Pause(); err != nil {
				m.handleError(err)
			}
			m.instanceChanged()
		})
		return m, nil
	case keys.KeyResume:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		if err := selected.Resume(); err != nil {
			return m, m.handleError(err)
		}
		return m, tea.WindowSize()
	case keys.KeyOpenIDE:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		// Open IDE at the instance's path and connect Claude
		cmd := m.openIDE(selected)
		return m, cmd
	case keys.KeyRebase:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Rebase session '%s' with main branch?", selected.Title)

		// Store the selected instance for the rebase
		m.pendingRebaseInstance = selected

		// Create a simple action that just returns a message to trigger the actual rebase
		rebaseAction := func() tea.Msg {
			return startRebaseMsg{}
		}

		return m, m.confirmAction(message, rebaseAction)
	case keys.KeyPRReview:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}

		// Check if instance is started
		if !selected.Started() {
			return m, m.handleError(fmt.Errorf("instance '%s' is not started", selected.Title))
		}

		// Check if instance is paused
		if selected.Paused() {
			return m, m.handleError(fmt.Errorf(instancePausedError, selected.Title))
		}

		// Get the worktree for the selected instance
		worktree, err := selected.GetGitWorktree()
		if err != nil {
			return m, m.handleError(fmt.Errorf("failed to get git worktree: %w", err))
		}

		// Get the worktree path
		worktreePath := worktree.GetWorktreePath()

		// Get current PR info from the worktree (always fresh)
		pr, err := git.GetCurrentPR(worktreePath)
		if err != nil {
			return m, m.handleError(fmt.Errorf(noPullRequestFoundError, err))
		}

		// Fetch PR comments (always fresh - includes resolved status detection)
		if err := pr.FetchComments(worktreePath); err != nil {
			return m, m.handleError(fmt.Errorf("failed to fetch PR comments: %w", err))
		}

		// Preprocess comments for better performance
		pr.PreprocessComments()

		// Show PR review UI
		m.state = statePRReview
		prReviewModel := ui.NewPRReviewModel(pr)
		m.prReviewOverlay = &prReviewModel

		// Initialize the PR review model
		initCmd := prReviewModel.Init()
		return m, initCmd
	case keys.KeyPRResolveConversations:
		return m.requestResolveAllConversationsConfirmation()
	case keys.KeyBookmark:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		// Show the bookmark creation state
		m.state = stateBookmark
		m.menu.SetState(ui.StateBookmark)
		m.textInputOverlay = overlay.NewTextInputOverlay("Enter bookmark message (or leave empty for auto-generated)", "")
		return m, nil
	case keys.KeyHistory:
		return m, m.showHistoryView()
	case keys.KeyTest:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		// Run Jest tests in the web directory
		cmd := m.runJestTests(selected)
		return m, cmd
	case keys.KeyGitStatus:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		// Show git status overlay
		return m, m.showGitStatusOverlay(selected)
	case keys.KeyGitStatusBookmark:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		// Show git status overlay in bookmark mode
		return m, m.showGitStatusOverlayBookmarkMode(selected)
	case keys.KeyCheckUpdate:
		// Trigger an immediate update check
		m.updateChecker.CheckNow()
		// For now, we'll just return without showing a message
		// The update indicator will appear in the menu when the check completes
		return m, nil
	case keys.KeyGitReset:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}

		// Check if instance is paused
		if selected.Paused() {
			return m, m.handleError(fmt.Errorf(instancePausedError, selected.Title))
		}

		// Get the worktree to get branch name
		worktree, err := selected.GetGitWorktree()
		if err != nil {
			return m, m.handleError(fmt.Errorf("failed to get git worktree: %w", err))
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Reset session '%s' to origin/%s?", selected.Title, worktree.GetBranchName())

		// Store the selected instance for the reset
		m.pendingResetInstance = selected

		// Create a simple action that just returns a message to trigger the actual reset
		resetAction := func() tea.Msg {
			return startGitResetMsg{}
		}

		return m, m.confirmAction(message, resetAction)
	case keys.KeyEnter:
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || !selected.TmuxAlive() {
			return m, nil
		}
		// Show help screen before attaching
		m.showHelpScreen(helpTypeInstanceAttach{}, func() {
			var ch chan struct{}
			var err error

			// Determine which pane to attach to based on active tab
			if m.tabbedWindow.IsInTerminalTab() {
				// If terminal tab is active, attach to terminal pane (pane 0)
				ch, err = m.list.AttachToPane(0)
			} else {
				// Otherwise, attach to AI pane (pane 1)
				ch, err = m.list.AttachToPane(1)
			}

			if err != nil {
				m.handleError(err)
				return
			}

			// Store selected instance for reload handling
			selected := m.list.GetSelectedInstance()

			<-ch
			m.state = stateDefault

			// Check if reload was requested (set by the tmux reload handler)
			if selected != nil && selected.NeedsReload() {
				selected.SetNeedsReload(false)
				// Reload the session
				if err := selected.ReloadSession(); err != nil {
					m.handleError(err)
					return
				}
				// Show a message that reload completed
				fmt.Fprintf(os.Stderr, "\n\033[32mSession reloaded. Press Enter to re-attach.\033[0m\n")
			}
		})
		return m, nil
	case keys.KeyListProgram:
		instances := m.list.GetInstances()
		if len(instances) == 0 {
			m.textOverlay = overlay.NewTextOverlay("Current program: " + m.program)
		} else {
			programSet := make(map[string]bool)
			for _, instance := range instances {
				programSet[instance.Program] = true
			}
			var programs []string
			for program := range programSet {
				programs = append(programs, "- "+program)
			}
			content := "Programs in use:\n" + strings.Join(programs, "\n")
			m.textOverlay = overlay.NewTextOverlay(content)
		}
		return m, nil
	default:
		return m, nil
	}
}
