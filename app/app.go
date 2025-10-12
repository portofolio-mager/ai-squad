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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const GlobalInstanceLimit = 10

var programs = []string{
	"claude",
	"codex",
	"gemini",
	"qwen",
	"crush",
	"coder",
	"opencode",
}

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

type state int

const (
	stateDefault state = iota
	// stateNew is the state when the user is creating a new instance.
	stateNew
	// statePrompt is the state when the user is entering a prompt.
	statePrompt
	// stateHelp is the state when a help screen is displayed.
	stateHelp
	// stateConfirm is the state when a confirmation modal is displayed.
	stateConfirm
	// stateChangeProgram is the state when the user is changing the program.
	stateChangeProgram
	// stateSelectProgram is the state when the user is selecting a program from a list.
	stateSelectProgram
	// stateBranchSelect is the state when the user is selecting a branch.
	stateBranchSelect
	// stateErrorLog is the state when displaying the error log.
	stateErrorLog
	// statePRReview is the state when reviewing PR comments.
	statePRReview
	// stateBookmark is the state when creating a bookmark commit.
	stateBookmark
	// stateHistory is the state when displaying the history overlay.
	stateHistory
	// stateKeybindingEditor is the state when editing keybindings.
	stateKeybindingEditor
	// stateGitStatus is the state when displaying the git status overlay.
	stateGitStatus
	// stateCommentDetail is the state when displaying full PR comment content.
	stateCommentDetail
)

type home struct {
	ctx context.Context

	// -- Storage and Configuration --

	program string
	autoYes bool

	// storage is the interface for saving/loading data to/from the app's state
	storage *session.Storage
	// appConfig stores persistent application configuration
	appConfig *config.Config
	// appState stores persistent application state like seen help screens
	appState config.AppState
	// updateChecker checks for application updates
	updateChecker *UpdateChecker

	// -- State --

	// state is the current discrete state of the application
	state state
	// scrollLocked indicates if up/down keys should scroll in diff view without shift
	scrollLocked bool
	// newInstanceFinalizer is called when the state is stateNew and then you press enter.
	// It registers the new instance in the list after the instance has been started.
	newInstanceFinalizer func()

	// promptAfterName tracks if we should enter prompt mode after naming
	promptAfterName bool

	// keySent is used to manage underlining menu items
	keySent bool

	// Window dimensions
	windowWidth  int
	windowHeight int

	// pendingCmd stores a command to be executed after confirmation
	pendingCmd tea.Cmd

	// -- UI Components --

	// list displays the list of instances
	list *ui.List
	// menu displays the bottom menu
	menu *ui.Menu
	// tabbedWindow displays the tabbed window with AI, diff, and terminal panes
	tabbedWindow *ui.TabbedWindow
	// errBox displays error messages
	errBox *ui.ErrBox
	// global spinner instance. we plumb this down to where it's needed
	spinner spinner.Model
	// startingSpinner is a transient spinner shown while an instance is being started.
	startingSpinner *spinner.Model
	// textInputOverlay handles text input with state
	textInputOverlay *overlay.TextInputOverlay
	// changeProgramOverlay handles program change input (legacy - kept for compatibility)
	changeProgramOverlay *overlay.TextInputOverlay
	// programListOverlay displays selectable programs for changing program
	programListOverlay *overlay.ProgramListOverlay
	// textOverlay displays text information
	textOverlay *overlay.TextOverlay
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay
	// branchSelectorOverlay displays branch selection interface
	branchSelectorOverlay *overlay.BranchSelectorOverlay
	// prReviewOverlay handles PR comment review
	prReviewOverlay *ui.PRReviewModel
	// historyOverlay displays scrollable history content
	historyOverlay *overlay.HistoryOverlay
	// commentDetailOverlay displays full PR comment content
	commentDetailOverlay *overlay.CommentDetailOverlay
	// keybindingEditorOverlay displays keybinding editor interface
	keybindingEditorOverlay *overlay.KeybindingEditorOverlay
	// gitStatusOverlay displays git status information
	gitStatusOverlay *overlay.GitStatusOverlay

	// errorLog stores all error messages for display
	errorLog []string

	// pendingRebaseInstance stores the instance to rebase after confirmation
	pendingRebaseInstance *session.Instance

	// pendingResetInstance stores the instance to reset after confirmation
	pendingResetInstance *session.Instance

	// rebaseInProgress indicates if a rebase is currently in progress
	rebaseInProgress bool
	// rebaseInstance is the instance being rebased
	rebaseInstance *session.Instance
	// rebaseBranchName is the branch being rebased
	rebaseBranchName string
	// rebaseOriginalSHA is the commit SHA before rebase started
	rebaseOriginalSHA string

	// -- Layout --

	// currentLayoutMode stores the effective layout mode
	currentLayoutMode config.LayoutMode
	// terminalWidth stores the current terminal width
	terminalWidth int
	// terminalHeight stores the current terminal height
	terminalHeight int
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

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	// Store window dimensions
	m.terminalWidth = msg.Width
	m.terminalHeight = msg.Height
	m.windowWidth = msg.Width
	m.windowHeight = msg.Height

	// Determine effective layout mode (always auto-detect)
	m.currentLayoutMode = m.appConfig.GetEffectiveLayoutMode(msg.Width)

	// Update menu mode based on layout
	m.menu.SetCompactMode(m.appConfig.ShouldUseCompactMenu(m.currentLayoutMode))
	m.menu.SetVerticalMode(m.currentLayoutMode == config.LayoutModeMobile)

	// Update logo visibility
	m.tabbedWindow.GetPreviewPane().SetHideLogo(m.appConfig.ShouldHideLogo(m.currentLayoutMode))

	if m.textInputOverlay != nil {
		m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}
	if m.textOverlay != nil {
		width, height := m.calculateOverlayDimensions()
		m.textOverlay.SetSize(width, height)
	}
	if m.historyOverlay != nil {
		m.historyOverlay.SetSize(int(float32(msg.Width)*0.9), int(float32(msg.Height)*0.9))
	}

	menuHeight := m.applyLayoutMode(msg.Width, msg.Height)

	// Update preview size for tmux sessions
	previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
	if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
		log.ErrorLog.Print(err)
	}
	m.menu.SetSize(msg.Width, menuHeight)
}

// applyLayoutMode applies the current layout mode to all UI components
// Returns the menu height for further use
func (m *home) applyLayoutMode(width, height int) int {
	var listWidth, tabsWidth, contentHeight, menuHeight int

	switch m.currentLayoutMode {
	case config.LayoutModeMobile:
		// Mobile: Vertical stacking layout
		// List takes full width but only 25% height (reduced from 30% to save space)
		// Preview/diff takes full width and remaining height
		// Menu gets a fixed compact height
		menuHeight = 2 // Fixed compact height for menu in mobile mode (reduced from 3)
		listHeight := int(float32(height) * 0.25)
		previewHeight := height - listHeight - menuHeight - 1

		// Ensure minimum heights for usability
		if listHeight < 3 {
			listHeight = 3
			previewHeight = height - listHeight - menuHeight - 1
		}
		if previewHeight < 5 {
			previewHeight = 5
			listHeight = height - previewHeight - menuHeight - 1
		}
		if menuHeight < 1 {
			menuHeight = 1
		}

		m.list.SetVerticalLayout(true)
		m.list.SetSize(width, listHeight)
		m.tabbedWindow.SetVerticalLayout(true)
		m.tabbedWindow.SetSize(width, previewHeight)

	default: // LayoutModeFull
		// Full: Standard 30/70 split
		listWidth = int(float32(width) * 0.3)
		tabsWidth = width - listWidth
		contentHeight = int(float32(height) * 0.9)
		menuHeight = height - contentHeight - 1

		// Ensure minimum widths for usability
		if listWidth < 20 {
			listWidth = 20
			tabsWidth = width - listWidth
		}
		if tabsWidth < 30 {
			tabsWidth = 30
			listWidth = width - tabsWidth
		}

		m.list.SetVerticalLayout(false)
		m.list.SetSize(listWidth, contentHeight)
		m.tabbedWindow.SetVerticalLayout(false)
		m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	}

	// Always set error box to 90% width
	m.errBox.SetSize(int(float32(width)*0.9), 1)

	// Adjust overlay sizes based on layout mode
	if m.textInputOverlay != nil {
		overlayWidth := int(float32(width) * 0.8)
		if m.currentLayoutMode == config.LayoutModeFull {
			overlayWidth = int(float32(width) * 0.6)
		}
		m.textInputOverlay.SetSize(overlayWidth, int(float32(height)*0.4))
	}
	if m.textOverlay != nil {
		overlayWidth := int(float32(width) * 0.8)
		if m.currentLayoutMode == config.LayoutModeFull {
			overlayWidth = int(float32(width) * 0.6)
		}
		m.textOverlay.SetWidth(overlayWidth)
	}

	return menuHeight
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

// handleTabSwitch handles tab switching in both forward and reverse directions
func (m *home) handleTabSwitch(reverse bool) (tea.Model, tea.Cmd) {
	if reverse {
		m.tabbedWindow.ToggleReverse()
	} else {
		m.tabbedWindow.Toggle()
	}
	m.menu.SetInDiffTab(m.tabbedWindow.IsInDiffTab())
	return m, m.instanceChanged()
}

// instanceChanged updates the AI pane, menu, diff pane, and terminal pane based on the selected instance. It returns an error
// Cmd if there was any error.
func (m *home) openIDE(instance *session.Instance) tea.Cmd {
	return func() tea.Msg {
		// Get the git worktree to access the worktree path
		gitWorktree, err := instance.GetGitWorktree()
		if err != nil {
			return fmt.Errorf("failed to get git worktree: %w", err)
		}

		// Open IDE at the worktree path (not the git root)
		worktreePath := gitWorktree.GetWorktreePath()

		// Get the IDE command from configuration
		globalConfig := m.appConfig
		ideCommand := config.GetEffectiveIdeCommand(worktreePath, globalConfig)

		cmd := exec.Command(ideCommand, worktreePath)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to open IDE (%s): %w", ideCommand, err)
		}

		return nil
	}
}

func (m *home) openFileInIDE(instance *session.Instance, filePath string) tea.Cmd {
	return func() tea.Msg {
		// Get the git worktree to access the worktree path
		gitWorktree, err := instance.GetGitWorktree()
		if err != nil {
			return fmt.Errorf("failed to get git worktree: %w", err)
		}

		// Construct the full path to the file using the worktree path
		worktreePath := gitWorktree.GetWorktreePath()
		fullPath := filepath.Join(worktreePath, filePath)

		// Get the IDE command from configuration
		globalConfig := m.appConfig
		ideCommand := config.GetEffectiveIdeCommand(worktreePath, globalConfig)

		// Open IDE with the specific file
		cmd := exec.Command(ideCommand, fullPath)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to open file in IDE (%s): %w", ideCommand, err)
		}

		return nil
	}
}

func (m *home) openFileInExternalDiff(instance *session.Instance, filePath string) tea.Cmd {
	return func() tea.Msg {
		// Get the git worktree to access the worktree path
		gitWorktree, err := instance.GetGitWorktree()
		if err != nil {
			return fmt.Errorf("failed to get git worktree: %w", err)
		}

		// Get the diff command from configuration
		worktreePath := gitWorktree.GetWorktreePath()
		globalConfig := m.appConfig
		diffCommand := config.GetEffectiveDiffCommand(worktreePath, globalConfig)

		if diffCommand == "" {
			return errors.New(noExternalDiffToolConfiguredError)
		}

		// Construct the full path to the file using the worktree path
		fullPath := filepath.Join(worktreePath, filePath)

		// Check if file exists
		if _, err := os.Stat(fullPath); err != nil {
			return fmt.Errorf("file not found: %s", fullPath)
		}

		// Split command and args to handle commands like "code --diff"
		parts := strings.Fields(diffCommand)
		if len(parts) == 0 {
			return fmt.Errorf("empty diff command")
		}
		cmd := exec.Command(parts[0], append(parts[1:], fullPath)...)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to open file in external diff tool (%s): %w", diffCommand, err)
		}

		return nil
	}
}

const (
	// maxBookmarkSummaryLen is the maximum length for auto-generated bookmark commit message summaries
	maxBookmarkSummaryLen = 100

	// Overlay dimension ratios
	overlayWidthRatio  = 0.8
	overlayHeightRatio = 0.9

	// Error messages
	cannotRebaseUncommittedChangesError = "cannot rebase: you have uncommitted changes. Press 'c' to checkout and commit, or stash them first"
	instancePausedError                 = "instance '%s' is paused. Press 'r' to resume it first"
	noPullRequestFoundError             = "no pull request found for this branch. Push the branch with 'p' first to create a PR: %w"
	noExternalDiffToolConfiguredError   = "no external diff tool configured. Set 'diff_command' in ~/.claude-squad/config.json or repository's CLAUDE.md"
)

func (m *home) createBookmarkCommit(instance *session.Instance, userMessage string) tea.Cmd {
	return func() tea.Msg {
		worktree, err := instance.GetGitWorktree()
		if err != nil {
			return fmt.Errorf("failed to get git worktree: %w", err)
		}

		// Get current branch name
		currentBranch, err := worktree.GetCurrentBranch()
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}

		var commitMessage string
		if userMessage != "" {
			// Use user-provided message
			commitMessage = fmt.Sprintf("[BOOKMARK] %s", userMessage)
		} else {
			// Generate message from commits since last bookmark
			lastBookmarkSHA, err := worktree.FindLastBookmarkCommit(currentBranch)
			if err != nil {
				return fmt.Errorf("failed to find last bookmark: %w", err)
			}

			// Get commit messages since last bookmark
			messages, err := worktree.GetCommitMessagesSince(lastBookmarkSHA, currentBranch)
			if err != nil {
				return fmt.Errorf("failed to get commit messages: %w", err)
			}

			if len(messages) == 0 {
				commitMessage = "[BOOKMARK] No changes since last bookmark"
			} else {
				// Generate a summary by concatenating the commit messages
				summary := strings.Join(messages, "; ")
				if len(summary) > maxBookmarkSummaryLen {
					summary = summary[:maxBookmarkSummaryLen-len("...")] + "..."
				}
				commitMessage = fmt.Sprintf("[BOOKMARK] %s", summary)
			}
		}

		// Create the bookmark commit (allow empty)
		if err := worktree.CreateBookmarkCommit(commitMessage); err != nil {
			return fmt.Errorf("failed to create bookmark commit: %w", err)
		}

		return instanceChangedMsg{}
	}
}

func (m *home) runJestTests(instance *session.Instance) tea.Cmd {
	return tea.Sequence(
		// First, switch to Jest tab
		func() tea.Msg {
			// Set the active tab to JestTab directly
			m.tabbedWindow.SetTab(ui.JestTab)
			m.menu.SetInDiffTab(false)
			return nil
		},
		// Then update the Jest pane with test results
		func() tea.Msg {
			m.tabbedWindow.UpdateJest(instance)
			return nil
		},
	)
}

// testStats holds test statistics
type testStats struct {
	passed int
	failed int
	total  int
}

// parseJestFinalStats parses the final test summary from Jest output
func parseJestFinalStats(output string) testStats {
	stats := testStats{}

	// Look for the test suites summary line
	// Example: "Test Suites: 1 passed, 1 failed, 2 total"
	re := regexp.MustCompile(`Test Suites:\s*(\d+)\s*passed(?:,\s*(\d+)\s*failed)?.*?,\s*(\d+)\s*total`)
	matches := re.FindStringSubmatch(output)

	if len(matches) >= 4 {
		if passed, err := strconv.Atoi(matches[1]); err == nil {
			stats.passed = passed
		}
		if len(matches) > 2 && matches[2] != "" {
			if failed, err := strconv.Atoi(matches[2]); err == nil {
				stats.failed = failed
			}
		}
		if total, err := strconv.Atoi(matches[3]); err == nil {
			stats.total = total
		}
	}

	// If no failed count was found, calculate it
	if stats.failed == 0 && stats.total > stats.passed {
		stats.failed = stats.total - stats.passed
	}

	return stats
}

func (m *home) instanceChanged() tea.Cmd {
	// selected may be nil
	selected := m.list.GetSelectedInstance()

	// Update the tabbed window with the current instance
	m.tabbedWindow.SetInstance(selected)

	m.tabbedWindow.UpdateDiff(selected)
	m.tabbedWindow.UpdateTerminal(selected)
	// Update menu with current instance
	m.menu.SetInstance(selected)

	// If there's no selected instance, we don't need to update the preview.
	if err := m.tabbedWindow.UpdatePreview(selected); err != nil {
		return m.handleError(err)
	}
	return nil
}

func (m *home) requestResolveAllConversationsConfirmation() (tea.Model, tea.Cmd) {
	selected := m.list.GetSelectedInstance()
	if selected == nil {
		return m, m.handleError(fmt.Errorf("no instance selected"))
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

	// Try to get PR and check for unresolved threads
	var threads []string
	var fetchError error
	pr, err := git.GetCurrentPR(worktreePath)
	if err == nil {
		// We have a PR, try to get unresolved threads
		threads, fetchError = pr.GetUnresolvedThreads(worktreePath)
	} else {
		fetchError = err
	}

	var message string
	timestamp := time.Now().Format("15:04:05")

	if fetchError == nil {
		if len(threads) == 0 {
			// No unresolved conversations
			m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] No unresolved review threads found on PR", timestamp))

			// For PR review state, just show error
			if m.state == statePRReview {
				m.errBox.SetError(errors.New("no unresolved review threads found on this pull request"))
				return m, func() tea.Msg {
					time.Sleep(2 * time.Second)
					return hideErrMsg{}
				}
			}
			// For main menu, return error
			return m, m.handleError(fmt.Errorf("no unresolved review threads found on this PR"))
		}
		message = fmt.Sprintf("Found %d unresolved review threads on this PR.\n\nAre you sure you want to resolve all %d threads?\n\nNote: Only review threads (line comments) can be resolved.\nGeneral PR comments cannot be resolved.\n\nThis action cannot be undone.", len(threads), len(threads))
	} else {
		// Log the error
		m.errorLog = append(m.errorLog, fmt.Sprintf("[%s] Error fetching thread count: %v", timestamp, fetchError))

		if strings.Contains(fetchError.Error(), "no pull request found") ||
			strings.Contains(fetchError.Error(), "no open pull requests") {
			message = fmt.Sprintf("Error: %v\n\nThis feature requires an open GitHub pull request for the current branch.\n\nMake sure you:\n1. Have an open PR for this branch\n2. Are authenticated with 'gh auth login'\n3. Are in a git repository", fetchError)

			// For main menu, return error immediately
			if m.state != statePRReview {
				return m, m.handleError(fetchError)
			}
		} else {
			// Some other error occurred, but we'll still allow the user to try
			message = "Unable to fetch thread count.\n\nAre you sure you want to resolve all review threads on this PR?\n\nNote: Only review threads (line comments) can be resolved.\nGeneral PR comments cannot be resolved.\n\nThis action cannot be undone."
		}
	}

	// Store the pending command to resolve conversations
	m.pendingCmd = func() tea.Msg {
		// When confirmed, send the message to resolve all conversations
		return ui.PRResolveAllConversationsMsg{}
	}

	// For PR review state, set confirmation state differently
	if m.state == statePRReview {
		m.state = stateConfirm
		m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
		return m, nil
	}

	// For main menu, use confirmAction
	return m, m.confirmAction(message, m.pendingCmd)
}

type keyupMsg struct{}

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

// hideErrMsg implements tea.Msg and clears the error text from the screen.
type hideErrMsg struct{}

// previewTickMsg implements tea.Msg and triggers a preview update
type previewTickMsg struct{}

type tickUpdateMetadataMessage struct{}

type instanceChangedMsg struct{}

// instanceStartResultMsg is sent when an asynchronous instance start completes.
type instanceStartResultMsg struct {
	Index       int
	Err         error
	PromptAfter bool
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

// startRebaseMsg is sent to trigger the actual rebase after confirmation
type startRebaseMsg struct{}

// startGitResetMsg is sent to trigger the actual git reset after confirmation
type startGitResetMsg struct{}

// remotePollingMsg is sent to check if the remote branch has been updated
type remotePollingMsg struct {
	branchName  string
	originalSHA string
}

// instanceCreatedMsg is sent when an instance has been created successfully
type instanceCreatedMsg struct {
	instance *session.Instance
	err      error
}

// instanceDeletedMsg is sent when an instance has been deleted successfully
type instanceDeletedMsg struct {
	title string
	err   error
}

// testResultsMsg is sent when test results are available
type testResultsMsg struct {
	output      string
	failedFiles []string
	err         error
}

// testStartedMsg is sent when tests start running
type testStartedMsg struct{}

// testProgressMsg is sent with test progress updates
type testProgressMsg struct {
	passed  int
	failed  int
	total   int
	running bool
}

// tickUpdateMetadataCmd is the callback to update the metadata of the instances every 500ms. Note that we iterate
// overall the instances and capture their output. It's a pretty expensive operation. Let's do it 2x a second only.
var tickUpdateMetadataCmd = func() tea.Msg {
	time.Sleep(500 * time.Millisecond)
	return tickUpdateMetadataMessage{}
}

// startInstanceAsync starts an instance asynchronously and returns a tea.Cmd
func (m *home) startInstanceAsync(instance *session.Instance) tea.Cmd {
	return func() tea.Msg {
		var resultErr error
		done := make(chan struct{})

		instance.StartAsync(true, func(err error) {
			resultErr = err
			close(done)
		})

		// Wait for completion
		<-done

		return instanceCreatedMsg{
			instance: instance,
			err:      resultErr,
		}
	}
}

// killInstanceAsync kills an instance asynchronously and returns a tea.Cmd
func (m *home) killInstanceAsync(instance *session.Instance) tea.Cmd {
	return func() tea.Msg {
		var resultErr error
		done := make(chan struct{})
		title := instance.Title

		instance.KillAsync(func(err error) {
			if err != nil {
				// If normal kill fails, try force kill
				log.InfoLog.Printf("Normal kill failed for %s: %v. Attempting force kill...", title, err)
				forceDone := make(chan struct{})
				var forceErr error

				instance.ForceKillAsync(func(err error) {
					forceErr = err
					close(forceDone)
				})

				<-forceDone

				if forceErr != nil {
					// Log the error but don't fail - we still want to remove the instance
					log.ErrorLog.Printf("Force kill encountered errors for %s: %v", title, forceErr)
					resultErr = nil // Set to nil so instance is removed from UI
				} else {
					// Force kill succeeded
					resultErr = nil
					log.InfoLog.Printf("Force kill succeeded for %s", title)
				}
			} else {
				resultErr = nil
			}
			close(done)
		})

		// Wait for completion
		<-done

		// Always remove from UI list after kill attempt
		// Even if there were errors, the instance should be considered gone
		m.list.Kill()

		return instanceDeletedMsg{
			title: title,
			err:   resultErr,
		}
	}
}

// calculateOverlayDimensions returns the width and height for overlay components
func (m *home) calculateOverlayDimensions() (width, height int) {
	width = int(float32(m.windowWidth) * overlayWidthRatio)
	height = int(float32(m.windowHeight) * overlayHeightRatio)
	return width, height
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

func (m *home) handleErrorLogState(_ tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key press closes the error log
	m.state = stateDefault
	m.textOverlay = nil
	return m, nil
}

// handleHistoryState handles key events when in history state
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

func (m *home) showTestResults(output string) {
	// Create text overlay with test results
	m.textOverlay = overlay.NewTextOverlay(output)
	m.state = stateHelp // Use help state since it handles text overlay display
	m.menu.SetState(ui.StateDefault)
}

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

func (m *home) createInstanceWithBranch(branchName string) (tea.Model, tea.Cmd) {
	// Create a unique title by adding a timestamp suffix
	// This prevents tmux session name conflicts when checking out the same branch multiple times
	timestamp := time.Now().Format("150405") // HHMMSS format
	title := fmt.Sprintf("%s-%s", branchName, timestamp)

	// Create a new instance with the selected branch
	instance, err := session.NewInstanceWithBranch(session.InstanceOptions{
		Title:      title,
		Path:       ".",
		Program:    m.program,
		BranchName: branchName,
	})
	if err != nil {
		m.state = stateDefault
		m.menu.SetState(ui.StateDefault)
		m.branchSelectorOverlay = nil
		return m, m.handleError(err)
	}

	m.newInstanceFinalizer = m.list.AddInstance(instance)
	m.list.SetSelectedInstance(m.list.NumInstances() - 1)
	m.branchSelectorOverlay = nil

	// Start the instance asynchronously
	cmd := m.startInstanceAsync(instance)

	// Save after adding new instance
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, m.handleError(err)
	}

	// Instance added successfully, call the finalizer
	m.newInstanceFinalizer()

	// Set state back to default
	m.state = stateDefault
	m.menu.SetState(ui.StateDefault)

	return m, tea.Batch(m.instanceChanged(), cmd)
}

// showHistoryView displays the history overlay for the current pane
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

// showGitStatusOverlay displays the git status overlay for the current instance
func (m *home) showGitStatusOverlay(instance *session.Instance) tea.Cmd {
	// Get the git worktree for the instance
	worktree, err := instance.GetGitWorktree()
	if err != nil {
		return m.handleError(fmt.Errorf("failed to get git worktree: %w", err))
	}

	// Get changed files for the branch
	files, err := worktree.GetChangedFilesForBranch()
	if err != nil {
		return m.handleError(fmt.Errorf("failed to get changed files: %w", err))
	}

	// Get the current branch name
	branchName, err := worktree.GetCurrentBranch()
	if err != nil {
		return m.handleError(fmt.Errorf("failed to get current branch: %w", err))
	}

	// Create the git status overlay
	m.gitStatusOverlay = overlay.NewGitStatusOverlay(branchName, files)
	m.gitStatusOverlay.OnDismiss = func() {
		m.state = stateDefault
		m.gitStatusOverlay = nil
	}

	// Set state to git status
	m.state = stateGitStatus

	return tea.WindowSize()
}

// showGitStatusOverlayBookmarkMode displays the git status overlay in bookmark mode
func (m *home) showGitStatusOverlayBookmarkMode(instance *session.Instance) tea.Cmd {
	// Get the git worktree for the instance
	worktree, err := instance.GetGitWorktree()
	if err != nil {
		return m.handleError(fmt.Errorf("failed to get git worktree: %w", err))
	}

	// Get the current branch name
	branchName, err := worktree.GetCurrentBranch()
	if err != nil {
		return m.handleError(fmt.Errorf("failed to get current branch: %w", err))
	}

	// Create the git status overlay in bookmark mode
	gitStatusOverlay, err := overlay.NewGitStatusOverlayBookmarkMode(branchName, worktree)
	if err != nil {
		return m.handleError(fmt.Errorf("failed to create bookmark git status overlay: %w", err))
	}

	m.gitStatusOverlay = gitStatusOverlay
	m.gitStatusOverlay.OnDismiss = func() {
		m.state = stateDefault
		m.gitStatusOverlay = nil
	}

	// Set state to git status
	m.state = stateGitStatus

	return tea.WindowSize()
}
