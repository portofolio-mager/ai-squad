package app

import (
	"ai-squad/config"
	"ai-squad/keys"
	"ai-squad/log"
	"ai-squad/session"
	"ai-squad/ui"
	"ai-squad/ui/overlay"
	"context"
	"fmt"
	"os"
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

	// -- State --

	// state is the current discrete state of the application
	state state
	// newInstanceFinalizer is called when the state is stateNew and then you press enter.
	// It registers the new instance in the list after the instance has been started.
	newInstanceFinalizer func()

	// promptAfterName tracks if we should enter prompt mode after naming
	promptAfterName bool

	// keySent is used to manage underlining menu items
	keySent bool

	// -- UI Components --

	// list displays the list of instances
	list *ui.List
	// menu displays the bottom menu
	menu *ui.Menu
	// tabbedWindow displays the tabbed window with preview and diff panes
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

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane()),
		errBox:       ui.NewErrBox(),
		storage:      storage,
		appConfig:    appConfig,
		program:      startProgram,
		autoYes:      autoYes,
		state:        stateDefault,
		appState:     appState,
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
	m.terminalWidth = msg.Width
	m.terminalHeight = msg.Height

	// Determine effective layout mode (always auto-detect)
	m.currentLayoutMode = m.appConfig.GetEffectiveLayoutMode(msg.Width)

	// Update menu mode based on layout
	m.menu.SetCompactMode(m.appConfig.ShouldUseCompactMenu(m.currentLayoutMode))
	m.menu.SetVerticalMode(m.currentLayoutMode == config.LayoutModeMobile)

	// Update logo visibility
	m.tabbedWindow.GetPreviewPane().SetHideLogo(m.appConfig.ShouldHideLogo(m.currentLayoutMode))

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
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.updateHandleWindowSizeEvent(msg)
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
	name, ok := keys.GlobalKeyStringsMap[msg.String()]
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

	if m.state == stateNew {
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
	} else if m.state == statePrompt {
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
	} else if m.state == stateChangeProgram {
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
	} else if m.state == stateSelectProgram {
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
		return m, nil
	}

	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.state = stateDefault
			m.confirmationOverlay = nil
			return m, nil
		}
		return m, nil
	}

	// Exit scrolling mode when ESC is pressed and preview pane is in scrolling mode
	// Check if Escape key was pressed and we're not in the diff tab (meaning we're in preview tab)
	// Always check for escape key first to ensure it doesn't get intercepted elsewhere
	if msg.Type == tea.KeyEsc {
		// If in preview tab and in scroll mode, exit scroll mode
		if !m.tabbedWindow.IsInDiffTab() && m.tabbedWindow.IsPreviewInScrollMode() {
			// Use the selected instance from the list
			selected := m.list.GetSelectedInstance()
			err := m.tabbedWindow.ResetPreviewToNormalMode(selected)
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

	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return m, nil
	}

	switch name {
	case keys.KeyHelp:
		return m.showHelpScreen(helpTypeGeneral{}, nil)
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
	case keys.KeyUp:
		m.list.Up()
		return m, m.instanceChanged()
	case keys.KeyDown:
		m.list.Down()
		return m, m.instanceChanged()
	case keys.KeyLeft:
		m.list.Left()
		return m, m.instanceChanged()
	case keys.KeyRight:
		m.list.Right()
		return m, m.instanceChanged()
	case keys.KeyShiftUp:
		m.tabbedWindow.ScrollUp()
		return m, m.instanceChanged()
	case keys.KeyShiftDown:
		m.tabbedWindow.ScrollDown()
		return m, m.instanceChanged()
	case keys.KeyTab:
		m.tabbedWindow.Toggle()
		m.menu.SetInDiffTab(m.tabbedWindow.IsInDiffTab())
		return m, m.instanceChanged()
	case keys.KeyKill:
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}

		// Create the kill action as a tea.Cmd
		killAction := func() tea.Msg {
			// Get worktree and check if branch is checked out
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}

			checkedOut, err := worktree.IsBranchCheckedOut()
			if err != nil {
				return err
			}

			if checkedOut {
				return fmt.Errorf("instance %s is currently checked out", selected.Title)
			}

			// Delete from storage first
			if err := m.storage.DeleteInstance(selected.Title); err != nil {
				return err
			}

			// Then kill the instance
			m.list.Kill()
			return instanceChangedMsg{}
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
			ch, err := m.list.Attach()
			if err != nil {
				m.handleError(err)
				return
			}
			<-ch
			m.state = stateDefault
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

// instanceChanged updates the preview pane, menu, and diff pane based on the selected instance. It returns an error
// Cmd if there was any error.
func (m *home) instanceChanged() tea.Cmd {
	// selected may be nil
	selected := m.list.GetSelectedInstance()

	m.tabbedWindow.UpdateDiff(selected)
	m.tabbedWindow.SetInstance(selected)
	// Update menu with current instance
	m.menu.SetInstance(selected)

	// If there's no selected instance, we don't need to update the preview.
	if err := m.tabbedWindow.UpdatePreview(selected); err != nil {
		return m.handleError(err)
	}
	return nil
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

// tickUpdateMetadataCmd is the callback to update the metadata of the instances every 500ms. Note that we iterate
// overall the instances and capture their output. It's a pretty expensive operation. Let's do it 2x a second only.
var tickUpdateMetadataCmd = func() tea.Msg {
	time.Sleep(500 * time.Millisecond)
	return tickUpdateMetadataMessage{}
}

// handleError handles all errors which get bubbled up to the app. sets the error message. We return a callback tea.Cmd that returns a hideErrMsg message
// which clears the error message after 3 seconds.
func (m *home) handleError(err error) tea.Cmd {
	log.ErrorLog.Printf("%v", err)
	m.errBox.SetError(err)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(3 * time.Second):
		}

		return hideErrMsg{}
	}
}

// confirmAction shows a confirmation modal and stores the action to execute on confirm
func (m *home) confirmAction(message string, action tea.Cmd) tea.Cmd {
	m.state = stateConfirm

	// Create and show the confirmation overlay using ConfirmationOverlay
	m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	// Set a fixed width for consistent appearance
	m.confirmationOverlay.SetWidth(50)

	// Set callbacks for confirmation and cancellation
	m.confirmationOverlay.OnConfirm = func() {
		m.state = stateDefault
		// Execute the action if it exists
		if action != nil {
			_ = action()
		}
	}

	m.confirmationOverlay.OnCancel = func() {
		m.state = stateDefault
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

	mainView := lipgloss.JoinVertical(
		lipgloss.Center,
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
		}
		return overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
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
	}

	return mainView
}
