package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"ai-squad/log"
	"ai-squad/session/git"
	"ai-squad/session/tmux"
)

type Status int

const (
	// Running is the status when the instance is running and claude is working.
	Running Status = iota
	// Ready is if the claude instance is ready to be interacted with (waiting for user input).
	Ready
	// Loading is if the instance is loading (if we are starting it up or something).
	Loading
	// Paused is if the instance is paused (worktree removed but branch preserved).
	Paused
	// Creating is if the instance is being created (worktree setup, tmux session creation).
	Creating
	// Deleting is if the instance is being deleted (cleanup in progress).
	Deleting
)

// Instance is a running instance of claude code.
type Instance struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Branch is the branch of the instance.
	Branch string
	// Status is the status of the instance.
	Status Status
	// Program is the program to run in the instance.
	Program string
	// Height is the height of the instance.
	Height int
	// Width is the width of the instance.
	Width int
	// CreatedAt is the time the instance was created.
	CreatedAt time.Time
	// UpdatedAt is the time the instance was last updated.
	UpdatedAt time.Time
	// AutoYes is true if the instance should automatically press enter when prompted.
	AutoYes bool
	// Prompt is the initial prompt to pass to the instance on startup
	Prompt string

	// In-memory cache for diff stats to avoid expensive git operations on every UI update
	diffStatsCache     *git.DiffStats
	diffStatsCacheTime time.Time

	// The below fields are initialized upon calling Start().

	started bool
	// tmuxSession is the tmux session for the instance.
	tmuxSession *tmux.TmuxSession
	// gitWorktree is the git worktree for the instance.
	gitWorktree *git.GitWorktree
	// existingBranch indicates if this instance is using an existing branch
	existingBranch bool
}

// ToInstanceData converts an Instance to its serializable form
func (i *Instance) ToInstanceData() InstanceData {
	data := InstanceData{
		Title:     i.Title,
		Path:      i.Path,
		Branch:    i.Branch,
		Status:    i.Status,
		Height:    i.Height,
		Width:     i.Width,
		CreatedAt: i.CreatedAt,
		UpdatedAt: time.Now(),
		Program:   i.Program,
		AutoYes:   i.AutoYes,
	}

	// Only include worktree data if gitWorktree is initialized
	if i.gitWorktree != nil {
		data.Worktree = GitWorktreeData{
			RepoPath:      i.gitWorktree.GetRepoPath(),
			WorktreePath:  i.gitWorktree.GetWorktreePath(),
			SessionName:   i.Title,
			BranchName:    i.gitWorktree.GetBranchName(),
			BaseCommitSHA: i.gitWorktree.GetBaseCommitSHA(),
		}
	}

	return data
}

// FromInstanceData creates a new Instance from serialized data
func FromInstanceData(data InstanceData) (*Instance, error) {
	instance := &Instance{
		Title:     data.Title,
		Path:      data.Path,
		Branch:    data.Branch,
		Status:    data.Status,
		Height:    data.Height,
		Width:     data.Width,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
		Program:   data.Program,
		gitWorktree: git.NewGitWorktreeFromStorage(
			data.Worktree.RepoPath,
			data.Worktree.WorktreePath,
			data.Worktree.SessionName,
			data.Worktree.BranchName,
			data.Worktree.BaseCommitSHA,
		),
	}

	if instance.Paused() {
		instance.started = true
		instance.tmuxSession = tmux.NewTmuxSession(instance.Title, instance.Program)
	} else {
		if err := instance.Start(false); err != nil {
			return nil, err
		}
	}

	return instance, nil
}

// Options for creating a new instance
type InstanceOptions struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Program is the program to run in the instance (e.g. "claude", "aider --model ollama_chat/gemma3:1b")
	Program string
	// If AutoYes is true, then
	AutoYes bool
	// BranchName is the name of an existing branch to checkout (optional)
	BranchName string
}

func NewInstance(opts InstanceOptions) (*Instance, error) {
	t := time.Now()

	// Convert path to absolute
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &Instance{
		Title:     opts.Title,
		Status:    Ready,
		Path:      absPath,
		Program:   opts.Program,
		Height:    0,
		Width:     0,
		CreatedAt: t,
		UpdatedAt: t,
		AutoYes:   false,
	}, nil
}

// NewInstanceWithBranch creates a new instance that will use an existing branch
func NewInstanceWithBranch(opts InstanceOptions) (*Instance, error) {
	t := time.Now()

	// Convert path to absolute
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// If title is empty, use branch name
	title := opts.Title
	if title == "" {
		title = opts.BranchName
	}

	instance := &Instance{
		Title:          title,
		Status:         Ready,
		Path:           absPath,
		Program:        opts.Program,
		Branch:         opts.BranchName, // Set the branch name
		Height:         0,
		Width:          0,
		CreatedAt:      t,
		UpdatedAt:      t,
		AutoYes:        opts.AutoYes,
		existingBranch: true, // Mark this as using an existing branch
	}

	return instance, nil
}

func (i *Instance) RepoName() (string, error) {
	if !i.started {
		return "", fmt.Errorf("cannot get repo name for instance that has not been started")
	}
	return i.gitWorktree.GetRepoName(), nil
}

func (i *Instance) SetStatus(status Status) {
	i.Status = status
}

// StartAsync starts the instance asynchronously, returning immediately.
// The onComplete callback is called when the operation completes (with error if failed).
func (i *Instance) StartAsync(firstTimeSetup bool, onComplete func(error)) {
	// Set status to Creating immediately
	i.SetStatus(Creating)

	// Run the actual start operation in a goroutine
	go func() {
		err := i.Start(firstTimeSetup)
		if err != nil {
			// Reset status on error
			i.SetStatus(Ready)
		}
		if onComplete != nil {
			onComplete(err)
		}
	}()
}

// firstTimeSetup is true if this is a new instance. Otherwise, it's one loaded from storage.
func (i *Instance) Start(firstTimeSetup bool) error {
	if i.Title == "" {
		return fmt.Errorf("instance title cannot be empty")
	}

	var tmuxSession *tmux.TmuxSession
	if i.tmuxSession != nil {
		// Use existing tmux session (useful for testing)
		tmuxSession = i.tmuxSession
	} else {
		// Create new tmux session
		tmuxSession = tmux.NewTmuxSession(i.Title, i.Program)
	}
	i.tmuxSession = tmuxSession

	if firstTimeSetup {
		if i.existingBranch && i.Branch != "" {
			// Create worktree for existing branch
			gitWorktree, _, err := git.NewGitWorktreeForBranch(i.Path, i.Title, i.Branch)
			if err != nil {
				return fmt.Errorf("failed to create git worktree for branch %s: %w", i.Branch, err)
			}
			i.gitWorktree = gitWorktree
		} else {
			// Create new worktree with auto-generated branch
			gitWorktree, branchName, err := git.NewGitWorktree(i.Path, i.Title)
			if err != nil {
				return fmt.Errorf("failed to create git worktree: %w", err)
			}
			i.gitWorktree = gitWorktree
			i.Branch = branchName
		}
	}

	// Setup error handler to cleanup resources on any error
	var setupErr error
	defer func() {
		if setupErr != nil {
			if cleanupErr := i.Kill(); cleanupErr != nil {
				setupErr = fmt.Errorf("%v (cleanup error: %v)", setupErr, cleanupErr)
			}
		} else {
			i.started = true
		}
	}()

	if !firstTimeSetup {
		// Reuse existing session
		if err := tmuxSession.Restore(); err != nil {
			setupErr = fmt.Errorf("failed to restore existing session: %w", err)
			return setupErr
		}
	} else {
		// Setup git worktree first
		if err := i.gitWorktree.Setup(); err != nil {
			setupErr = fmt.Errorf("failed to setup git worktree: %w", err)
			return setupErr
		}

		// Create new session
		sessionPath, err := i.GetSessionPath()
		if err != nil {
			setupErr = fmt.Errorf("failed to get session path: %w", err)
			return setupErr
		}
		if err := i.tmuxSession.Start(sessionPath); err != nil {
			// Cleanup git worktree if tmux session creation fails
			if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
			setupErr = fmt.Errorf("failed to start new session: %w", err)
			return setupErr
		}
	}

	i.SetStatus(Running)

	return nil
}

// KillAsync terminates the instance asynchronously, returning immediately.
// The onComplete callback is called when the operation completes (with error if failed).
func (i *Instance) KillAsync(onComplete func(error)) {
	// Set status to Deleting immediately
	i.SetStatus(Deleting)

	// Run the actual kill operation in a goroutine
	go func() {
		err := i.Kill()
		if err != nil {
			// Reset status on error
			i.SetStatus(Ready)
		}
		if onComplete != nil {
			onComplete(err)
		}
	}()
}

// ForceKillAsync terminates the instance with force, using aggressive cleanup methods
// The onComplete callback is called when the operation completes (with error if failed).
func (i *Instance) ForceKillAsync(onComplete func(error)) {
	// Set status to Deleting immediately
	i.SetStatus(Deleting)

	// Run the actual force kill operation in a goroutine
	go func() {
		err := i.ForceKill()
		// Force kill always succeeds, so don't reset status on error
		if onComplete != nil {
			onComplete(err)
		}
	}()
}

// Kill terminates the instance and cleans up all resources
func (i *Instance) Kill() error {
	if !i.started {
		// If instance was never started, just return success
		return nil
	}

	var errs []error

	// Always try to cleanup both resources, even if one fails
	// Clean up tmux session first since it's using the git worktree
	if i.tmuxSession != nil {
		if err := i.tmuxSession.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close tmux session: %w", err))
		}
	}

	// Then clean up git worktree
	if i.gitWorktree != nil {
		if err := i.gitWorktree.Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("failed to cleanup git worktree: %w", err))
		}
	}

	return i.combineErrors(errs)
}

// ForceKill terminates the instance using aggressive cleanup methods
// This method attempts to ensure cleanup succeeds even if standard methods fail
func (i *Instance) ForceKill() error {
	if !i.started {
		// If instance was never started, just return success
		return nil
	}

	var errs []error

	// First, try normal kill
	if err := i.Kill(); err == nil {
		// Normal kill succeeded, we're done
		return nil
	} else {
		errs = append(errs, fmt.Errorf("normal kill failed: %w", err))
	}

	// If normal kill failed, try more aggressive cleanup methods

	// Force close tmux session
	if i.tmuxSession != nil {
		// Try to force kill the tmux session
		sessionName := i.tmuxSession.GetSessionName()
		if sessionName != "" {
			// Check if session exists before trying to kill it
			checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
			if checkErr := checkCmd.Run(); checkErr == nil {
				// Session exists, try to kill it
				killCmd := exec.Command("tmux", "kill-session", "-t", sessionName)
				if err := killCmd.Run(); err != nil {
					errs = append(errs, fmt.Errorf("failed to force kill tmux session: %w", err))
				}
			}
		}
	}

	// Force cleanup git worktree
	if i.gitWorktree != nil {
		// Try force cleanup
		if err := i.gitWorktree.ForceCleanup(); err != nil {
			errs = append(errs, fmt.Errorf("git worktree force cleanup failed: %w", err))

			// If git cleanup failed, try manual filesystem cleanup
			worktreePath := i.gitWorktree.GetWorktreePath()
			if worktreePath != "" && worktreePath != "/" && strings.Contains(worktreePath, "worktrees") {
				if err := os.RemoveAll(worktreePath); err != nil {
					errs = append(errs, fmt.Errorf("failed to manually remove worktree directory: %w", err))
				}
			}
		}
	}

	// Mark as not started regardless of errors
	i.started = false

	// Return combined errors but consider the operation successful
	// (instance is effectively killed even if cleanup wasn't perfect)
	if len(errs) > 0 {
		return fmt.Errorf("force kill completed with errors: %w", i.combineErrors(errs))
	}

	return nil
}

// combineErrors combines multiple errors into a single error
func (i *Instance) combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple cleanup errors occurred:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}

func (i *Instance) Preview() (string, error) {
	if i.tmuxSession == nil || i.Status == Paused {
		return "", nil
	}

	// Check for nil gitWorktree
	if i.gitWorktree == nil {
		return "", nil
	}

	// Check if tmux session was killed and needs to be recreated
	if err := i.ensureTmuxSession(); err != nil {
		return "", err
	}

	// Ensure terminal pane exists first
	i.tmuxSession.CreateTerminalPane(i.gitWorktree.GetWorktreePath())
	// AI content is in pane 1 after terminal split
	return i.tmuxSession.CaptureTerminalContent()
}

func (i *Instance) HasUpdated() (updated bool, hasPrompt bool) {
	if !i.started {
		return false, false
	}

	// Check if tmux session still exists
	if !i.tmuxSession.DoesSessionExist() {
		return false, false
	}

	return i.tmuxSession.HasUpdated()
}

// TapEnter sends an enter key press to the tmux session if AutoYes is enabled.
func (i *Instance) TapEnter() {
	if !i.started || !i.AutoYes {
		return
	}
	if err := i.tmuxSession.TapEnter(); err != nil {
		log.ErrorLog.Printf("error tapping enter: %v", err)
	}
}

func (i *Instance) Attach() (chan struct{}, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot attach instance that has not been started")
	}

	// Check if tmux session was killed and needs to be recreated
	if err := i.ensureTmuxSession(); err != nil {
		return nil, err
	}

	return i.tmuxSession.Attach()
}

// AttachToPane attaches to the instance and focuses on the specified pane
func (i *Instance) AttachToPane(paneIndex int) (chan struct{}, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot attach instance that has not been started")
	}

	// Check if tmux session was killed and needs to be recreated
	if err := i.ensureTmuxSession(); err != nil {
		return nil, err
	}

	// If attaching to terminal pane, ensure it exists first
	if paneIndex == 1 {
		if err := i.tmuxSession.CreateTerminalPane(i.gitWorktree.GetWorktreePath()); err != nil {
			// Log error but continue - attach will handle missing pane
			log.ErrorLog.Printf("failed to create terminal pane: %v", err)
		}
	}

	return i.tmuxSession.AttachToPane(paneIndex)
}

// GetTerminalContent returns the content of the terminal pane
func (i *Instance) GetTerminalContent() (string, error) {
	if !i.started || i.Status == Paused {
		return "", fmt.Errorf("instance not available")
	}

	// Check if tmux session was killed and needs to be recreated
	if err := i.ensureTmuxSession(); err != nil {
		return "", err
	}

	// Ensure terminal pane exists
	if err := i.tmuxSession.CreateTerminalPane(i.gitWorktree.GetWorktreePath()); err != nil {
		return "", fmt.Errorf("failed to create terminal pane: %v", err)
	}

	// Terminal is in pane 0 (original pane)
	return i.tmuxSession.CapturePaneContent()
}

// GetTerminalFullHistory captures the entire terminal pane output including full scrollback history
func (i *Instance) GetTerminalFullHistory() (string, error) {
	if !i.started || i.Status == Paused {
		return "", fmt.Errorf("instance not available")
	}

	// Check if tmux session was killed and needs to be recreated
	if err := i.ensureTmuxSession(); err != nil {
		return "", err
	}

	// Ensure terminal pane exists
	if err := i.tmuxSession.CreateTerminalPane(i.gitWorktree.GetWorktreePath()); err != nil {
		return "", fmt.Errorf("failed to create terminal pane: %v", err)
	}

	// Terminal is in pane 0, capture with full history (-S - means from start of history)
	// We need to specify the target pane explicitly
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-t", i.tmuxSession.GetSessionName()+".0")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to capture terminal full history: %v", err)
	}
	return string(output), nil
}

// GetAIFullHistory captures the entire AI pane output including full scrollback history
func (i *Instance) GetAIFullHistory() (string, error) {
	if !i.started || i.Status == Paused {
		return "", fmt.Errorf("instance not available")
	}

	// Check if tmux session was killed and needs to be recreated
	if err := i.ensureTmuxSession(); err != nil {
		return "", err
	}

	// Ensure terminal pane exists
	if err := i.tmuxSession.CreateTerminalPane(i.gitWorktree.GetWorktreePath()); err != nil {
		return "", fmt.Errorf("failed to create terminal pane: %v", err)
	}

	// AI is in pane 1, capture with full history (-S - means from start of history)
	// We need to specify the target pane explicitly
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-t", i.tmuxSession.GetSessionName()+".1")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to capture AI full history: %v", err)
	}
	return string(output), nil
}

func (i *Instance) SetPreviewSize(width, height int) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot set preview size for instance that has not been started or " +
			"is paused")
	}
	return i.tmuxSession.SetDetachedSize(width, height)
}

// GetGitWorktree returns the git worktree for the instance
func (i *Instance) GetGitWorktree() (*git.GitWorktree, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot get git worktree for instance that has not been started")
	}
	return i.gitWorktree, nil
}

func (i *Instance) Started() bool {
	return i.started
}

// SetTitle sets the title of the instance. Returns an error if the instance has started.
// We cant change the title once it's been used for a tmux session etc.
func (i *Instance) SetTitle(title string) error {
	if i.started {
		return fmt.Errorf("cannot change title of a started instance")
	}
	i.Title = title
	return nil
}

func (i *Instance) Paused() bool {
	return i.Status == Paused
}

// TmuxAlive returns true if the tmux session is alive. This is a sanity check before attaching.
func (i *Instance) TmuxAlive() bool {
	return i.tmuxSession.DoesSessionExist()
}

// ensureTmuxSession checks if the tmux session exists and recreates it if needed
func (i *Instance) ensureTmuxSession() error {
	if i.gitWorktree == nil {
		return fmt.Errorf("cannot ensure tmux session: gitWorktree is nil")
	}

	if !i.tmuxSession.DoesSessionExist() {
		log.InfoLog.Printf("tmux session %s was killed, recreating in %s...", i.tmuxSession.GetSessionName(), i.gitWorktree.GetWorktreePath())
		// Recreate the session in the correct worktree directory
		if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
			return fmt.Errorf("failed to recreate tmux session: %v", err)
		}
	}
	return nil
}

// GetReloadChannel returns the reload channel for handling Ctrl+R
func (i *Instance) GetReloadChannel() <-chan struct{} {
	if i.tmuxSession == nil {
		return nil
	}
	return i.tmuxSession.GetReloadChannel()
}

// ReloadSession kills and recreates the tmux session
func (i *Instance) ReloadSession() error {
	if !i.started {
		return fmt.Errorf("cannot reload session that has not been started")
	}

	if i.gitWorktree == nil {
		return fmt.Errorf("cannot reload session: gitWorktree is nil")
	}

	log.InfoLog.Printf("Reloading tmux session %s in %s...", i.tmuxSession.GetSessionName(), i.gitWorktree.GetWorktreePath())
	return i.tmuxSession.ReloadSession(i.gitWorktree.GetWorktreePath())
}

// NeedsReload returns true if the instance needs to be reloaded
func (i *Instance) NeedsReload() bool {
	if i.tmuxSession == nil {
		return false
	}
	return i.tmuxSession.NeedsReload()
}

// SetNeedsReload sets whether the instance needs to be reloaded
func (i *Instance) SetNeedsReload(needs bool) {
	if i.tmuxSession != nil && !needs {
		i.tmuxSession.ClearReloadFlag()
	}
}

// Pause stops the tmux session and removes the worktree, preserving the branch
func (i *Instance) Pause() error {
	if !i.started {
		return fmt.Errorf("cannot pause instance that has not been started")
	}
	if i.Status == Paused {
		return fmt.Errorf("instance is already paused")
	}

	var errs []error

	// Check if there are any changes to commit
	if dirty, err := i.gitWorktree.IsDirty(); err != nil {
		errs = append(errs, fmt.Errorf("failed to check if worktree is dirty: %w", err))
		log.ErrorLog.Print(err)
	} else if dirty {
		// Commit changes locally (without pushing to GitHub)
		commitMsg := fmt.Sprintf("[aisquad] update from '%s' on %s (paused)", i.Title, time.Now().Format(time.RFC822))
		if err := i.gitWorktree.CommitChanges(commitMsg); err != nil {
			errs = append(errs, fmt.Errorf("failed to commit changes: %w", err))
			log.ErrorLog.Print(err)
			// Return early if we can't commit changes to avoid corrupted state
			return i.combineErrors(errs)
		}
	}

	// Detach from tmux session instead of closing to preserve session output
	if err := i.tmuxSession.DetachSafely(); err != nil {
		errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
		log.ErrorLog.Print(err)
		// Continue with pause process even if detach fails
	}

	// Check if worktree exists before trying to remove it
	if _, err := os.Stat(i.gitWorktree.GetWorktreePath()); err == nil {
		// Remove worktree but keep branch
		if err := i.gitWorktree.Remove(); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove git worktree: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}

		// Only prune if remove was successful
		if err := i.gitWorktree.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("failed to prune git worktrees: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}
	}

	if err := i.combineErrors(errs); err != nil {
		log.ErrorLog.Print(err)
		return err
	}

	i.SetStatus(Paused)
	// Invalidate cache when pausing
	i.diffStatsCache = nil
	i.diffStatsCacheTime = time.Time{}
	_ = clipboard.WriteAll(i.gitWorktree.GetBranchName())
	return nil
}

// Resume recreates the worktree and restarts the tmux session
func (i *Instance) Resume() error {
	if !i.started {
		return fmt.Errorf("cannot resume instance that has not been started")
	}
	if i.Status != Paused {
		return fmt.Errorf("can only resume paused instances")
	}

	// Check if branch is checked out
	if checked, err := i.gitWorktree.IsBranchCheckedOut(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to check if branch is checked out: %w", err)
	} else if checked {
		return fmt.Errorf("cannot resume: branch is checked out, please switch to a different branch")
	}

	// Setup git worktree
	if err := i.gitWorktree.Setup(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to setup git worktree: %w", err)
	}

	// Check if tmux session still exists from pause, otherwise create new one
	if i.tmuxSession.DoesSessionExist() {
		// Session exists, just restore PTY connection to it
		if err := i.tmuxSession.Restore(); err != nil {
			log.ErrorLog.Print(err)
			// If restore fails, fall back to creating new session
			sessionPath, err := i.GetSessionPath()
			if err != nil {
				return fmt.Errorf("failed to get session path: %w", err)
			}
			if err := i.tmuxSession.Start(sessionPath); err != nil {
				log.ErrorLog.Print(err)
				// Cleanup git worktree if tmux session creation fails
				if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
					err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
					log.ErrorLog.Print(err)
				}
				return fmt.Errorf("failed to start new session: %w", err)
			}
		}
	} else {
		// Create new tmux session
		sessionPath, err := i.GetSessionPath()
		if err != nil {
			return fmt.Errorf("failed to get session path: %w", err)
		}
		if err := i.tmuxSession.Start(sessionPath); err != nil {
			log.ErrorLog.Print(err)
			// Cleanup git worktree if tmux session creation fails
			if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
				log.ErrorLog.Print(err)
			}
			return fmt.Errorf("failed to start new session: %w", err)
		}
	}

	i.SetStatus(Running)
	return nil
}

// diffStatsCacheTTL defines how long the diff stats cache is valid
const diffStatsCacheTTL = time.Second

// UpdateDiffStats updates the cached git diff statistics for this instance
func (i *Instance) UpdateDiffStats() error {
	if !i.started {
		i.diffStatsCache = nil
		return nil
	}

	if i.Status == Paused {
		// Keep the previous diff stats if the instance is paused
		return nil
	}

	// Check if cache is still fresh
	if i.diffStatsCache != nil && time.Since(i.diffStatsCacheTime) < diffStatsCacheTTL {
		return nil
	}

	stats := i.gitWorktree.Diff()
	if stats.Error != nil {
		if strings.Contains(stats.Error.Error(), "base commit SHA not set") {
			// Worktree is not fully set up yet, not an error
			i.diffStatsCache = nil
			i.diffStatsCacheTime = time.Now()
			return nil
		}
		return fmt.Errorf("failed to get diff stats: %w", stats.Error)
	}

	i.diffStatsCache = stats
	i.diffStatsCacheTime = time.Now()
	return nil
}

// GetDiffStats returns the cached git diff statistics
func (i *Instance) GetDiffStats() *git.DiffStats {
	if !i.started {
		return nil
	}
	return i.diffStatsCache
}

// GetLastCommitDiffStats returns the diff statistics for uncommitted changes if they exist, otherwise the last commit
func (i *Instance) GetLastCommitDiffStats() *git.DiffStats {
	if !i.started {
		return nil
	}

	if i.Status == Paused {
		// For paused instances, we can still get the diff
		return i.gitWorktree.DiffUncommittedOrLastCommit()
	}

	return i.gitWorktree.DiffUncommittedOrLastCommit()
}

// GetCommitDiffAtOffset returns the diff statistics for a commit at the specified offset
// offset -1 = uncommitted changes, offset 0 = HEAD, offset 1 = HEAD~1, etc.
func (i *Instance) GetCommitDiffAtOffset(offset int) *git.DiffStats {
	if !i.started {
		return nil
	}

	if offset == -1 {
		uncommittedStats := i.gitWorktree.DiffUncommitted()
		// If there are no uncommitted changes, show the last commit instead
		if uncommittedStats.IsEmpty() && uncommittedStats.Error == nil {
			// Return the HEAD commit stats, but preserve that we tried to get uncommitted changes
			headStats := i.gitWorktree.DiffCommitAtOffset(0)
			if headStats != nil {
				// Mark that this is showing HEAD because there were no uncommitted changes
				headStats.IsUncommitted = false
			}
			return headStats
		}
		uncommittedStats.IsUncommitted = true
		return uncommittedStats
	}

	return i.gitWorktree.DiffCommitAtOffset(offset)
}

// GetCommitInfo returns the commit hash and message at the specified offset
func (i *Instance) GetCommitInfo(offset int) (hash string, message string, err error) {
	if !i.started {
		return "", "", fmt.Errorf("instance not started")
	}

	return i.gitWorktree.GetCommitInfo(offset)
}

// GetSessionPath returns the path where tmux session should start
// It combines the worktree path with the subdirectory if AI Squad was run from a subdirectory
func (i *Instance) GetSessionPath() (string, error) {
	if i.gitWorktree == nil {
		return "", fmt.Errorf("git worktree not initialized")
	}

	worktreePath := i.gitWorktree.GetWorktreePath()
	repoPath := i.gitWorktree.GetRepoPath()

	relPath, err := filepath.Rel(repoPath, i.Path)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %w", err)
	}

	return filepath.Join(worktreePath, relPath), nil
}

// SendPrompt sends a prompt to the tmux session
func (i *Instance) SendPrompt(prompt string) error {
	if !i.started {
		return fmt.Errorf("instance not started")
	}
	if i.tmuxSession == nil {
		return fmt.Errorf("tmux session not initialized")
	}
	if err := i.tmuxSession.SendKeys(prompt); err != nil {
		return fmt.Errorf("error sending keys to tmux session: %w", err)
	}

	// Brief pause to prevent carriage return from being interpreted as newline
	time.Sleep(100 * time.Millisecond)
	if err := i.tmuxSession.TapEnter(); err != nil {
		return fmt.Errorf("error tapping enter: %w", err)
	}

	// Invalidate cache when sending a prompt as git state might change
	i.diffStatsCache = nil
	i.diffStatsCacheTime = time.Time{}

	return nil
}

// SendPromptToAI sends a prompt directly to the AI pane (pane 1)
func (i *Instance) SendPromptToAI(prompt string) error {
	if !i.started {
		return fmt.Errorf("instance not started")
	}
	if i.tmuxSession == nil {
		return fmt.Errorf("tmux session not initialized")
	}

	// Debug: Check if tmux session exists
	if !i.tmuxSession.DoesSessionExist() {
		return fmt.Errorf("tmux session does not exist")
	}

	log.WarningLog.Printf("Sending prompt to AI pane: %s", prompt[:min(50, len(prompt))])

	if i.gitWorktree == nil {
		return fmt.Errorf("cannot send prompt: gitWorktree is nil")
	}

	// First ensure the terminal pane exists (creates split if needed)
	if err := i.tmuxSession.CreateTerminalPane(i.gitWorktree.GetWorktreePath()); err != nil {
		log.ErrorLog.Printf("Failed to create terminal pane: %v", err)
		return fmt.Errorf("error creating terminal pane: %w", err)
	}

	// Send the prompt to the AI pane using tmux send-keys with Enter
	if err := i.tmuxSession.SendKeysToTerminal(prompt); err != nil {
		log.ErrorLog.Printf("Failed to send keys to AI pane: %v", err)
		return fmt.Errorf("error sending keys to AI pane: %w", err)
	}

	// Brief pause to prevent carriage return from being interpreted as newline
	time.Sleep(100 * time.Millisecond)

	// Send Enter to the AI pane
	if err := i.tmuxSession.SendKeysToTerminal("Enter"); err != nil {
		log.ErrorLog.Printf("Failed to send enter to AI pane: %v", err)
		return fmt.Errorf("error sending enter to AI pane: %w", err)
	}

	log.WarningLog.Printf("Successfully sent prompt and enter to AI pane")
	return nil
}

// PreviewFullHistory captures the entire tmux pane output including full scrollback history
func (i *Instance) PreviewFullHistory() (string, error) {
	if i.tmuxSession == nil || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContentWithOptions("-", "-")
}

// SetTmuxSession sets the tmux session for testing purposes
func (i *Instance) SetTmuxSession(session *tmux.TmuxSession) {
	i.tmuxSession = session
}

// SendKeys sends keys to the tmux session
func (i *Instance) SendKeys(keys string) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot send keys to instance that has not been started or is paused")
	}
	return i.tmuxSession.SendKeys(keys)
}
