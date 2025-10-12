package app

import (
	"ai-squad/config"
	"ai-squad/session"
	"ai-squad/ui"
	"ai-squad/ui/overlay"
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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
	// changeProgramOverlay *overlay.TextInputOverlay
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

// Message types
type (
	keyupMsg                  struct{}
	previewTickMsg            struct{}
	tickUpdateMetadataMessage struct{}
	instanceChangedMsg        struct{}
	hideErrMsg                struct{}

	// instanceStartResultMsg is sent when an asynchronous instance start completes.
	instanceStartResultMsg struct {
		Index       int
		Err         error
		PromptAfter bool
	}

	// startRebaseMsg is sent to trigger the actual rebase after confirmation
	startRebaseMsg struct{}

	// startGitResetMsg is sent to trigger the actual git reset after confirmation
	startGitResetMsg struct{}

	// remotePollingMsg is sent to check if the remote branch has been updated
	remotePollingMsg struct {
		branchName  string
		originalSHA string
	}

	// instanceCreatedMsg is sent when an instance has been created successfully
	instanceCreatedMsg struct {
		instance *session.Instance
		err      error
	}

	// instanceDeletedMsg is sent when an instance has been deleted successfully
	instanceDeletedMsg struct {
		title string
		err   error
	}

	// testResultsMsg is sent when test results are available
	testResultsMsg struct {
		output      string
		failedFiles []string
		err         error
	}

	// testStartedMsg is sent when tests start running
	testStartedMsg struct{}

	// testProgressMsg is sent with test progress updates
	testProgressMsg struct {
		passed  int
		failed  int
		total   int
		running bool
	}
)

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
	noExternalDiffToolConfiguredError   = "no external diff tool configured. Set 'diff_command' in ~/.ai-squad/config.json or repository's CLAUDE.md"
)
