package app

import (
	"ai-squad/config"
	"ai-squad/log"
	"ai-squad/session"
	"ai-squad/ui"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// instanceChanged updates the AI pane, menu, diff pane, and terminal pane based on the selected instance. It returns an error
// Cmd if there was any error.
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

// openIDE opens the IDE at the instance's worktree path
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

// openFileInIDE opens a specific file in the IDE
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

// createInstanceWithBranch creates a new instance with the selected branch
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
