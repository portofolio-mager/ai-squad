package app

import (
	"ai-squad/config"
	"ai-squad/session"
	"ai-squad/ui"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// testStats holds test statistics
type testStats struct {
	passed int
	failed int
	total  int
}

// runJestTests runs Jest tests and switches to the Jest tab
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

// openFileInExternalDiff opens a file in the configured external diff tool
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
