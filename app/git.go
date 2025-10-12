package app

import (
	"ai-squad/session"
	"ai-squad/session/git"
	"ai-squad/ui"
	"ai-squad/ui/overlay"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// createBookmarkCommit creates a bookmark commit with optional user message
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

// requestResolveAllConversationsConfirmation shows a confirmation dialog for resolving all PR conversations
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
