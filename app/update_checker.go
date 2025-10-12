package app

import (
	"ai-squad/log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// UpdateChecker manages checking for application updates
type UpdateChecker struct {
	mu                 sync.RWMutex
	updateAvailable    bool
	lastCheck          time.Time
	checkInterval      time.Duration
	currentCommitCount int
	remoteCommitCount  int
}

// NewUpdateChecker creates a new update checker instance
func NewUpdateChecker() *UpdateChecker {
	return &UpdateChecker{
		checkInterval: 30 * time.Minute,
	}
}

// IsUpdateAvailable returns whether an update is available
func (uc *UpdateChecker) IsUpdateAvailable() bool {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	return uc.updateAvailable
}

// GetCommitsBehind returns how many commits the local branch is behind
func (uc *UpdateChecker) GetCommitsBehind() int {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	if uc.remoteCommitCount > uc.currentCommitCount {
		return uc.remoteCommitCount - uc.currentCommitCount
	}
	return 0
}

// StartBackgroundCheck starts checking for updates in the background
func (uc *UpdateChecker) StartBackgroundCheck() {
	go func() {
		// Do initial check
		uc.checkForUpdates()

		// Set up periodic checking
		ticker := time.NewTicker(uc.checkInterval)
		defer ticker.Stop()

		for range ticker.C {
			uc.checkForUpdates()
		}
	}()
}

// CheckNow forces an immediate update check
func (uc *UpdateChecker) CheckNow() {
	go uc.checkForUpdates()
}

// checkForUpdates performs the actual update check
func (uc *UpdateChecker) checkForUpdates() {
	// Try different methods to find the ai-squad git repository
	gitRoot := ""

	// Method 1: Check if we're already in a git repository (development mode)
	if currentRoot := findGitRoot("."); currentRoot != "" {
		// Verify this is ai-squad by checking for specific files
		if isAISquadRepo(currentRoot) {
			gitRoot = currentRoot
		}
	}

	// Method 2: Check common installation locations
	if gitRoot == "" {
		commonPaths := []string{
			"/usr/local/src/ai-squad",
			"/opt/ai-squad",
			"~/src/ai-squad",
			"~/ai-squad",
		}

		for _, path := range commonPaths {
			expandedPath := os.ExpandEnv(strings.Replace(path, "~", "$HOME", 1))
			if root := findGitRoot(expandedPath); root != "" && isAISquadRepo(root) {
				gitRoot = root
				break
			}
		}
	}

	// Method 3: Try to find via the executable path if csa is in PATH
	if gitRoot == "" {
		if execPath, err := exec.LookPath("csa"); err == nil {
			// Resolve symlinks to get the actual path
			resolvedPath, err := exec.Command("readlink", "-f", execPath).Output()
			if err != nil {
				// Try alternative for macOS
				resolvedPath, err = exec.Command("realpath", execPath).Output()
			}

			if err == nil {
				// Extract the directory containing the executable
				execDir := strings.TrimSpace(string(resolvedPath))
				if idx := strings.LastIndex(execDir, "/"); idx != -1 {
					execDir = execDir[:idx]
				}

				// Search up from the executable location
				if root := findGitRoot(execDir); root != "" && isAISquadRepo(root) {
					gitRoot = root
				}
			}
		}
	}

	if gitRoot == "" {
		// Silently return - this is normal for binary distributions
		return
	}

	// Fetch latest changes from origin
	cmd := exec.Command("git", "-C", gitRoot, "fetch", "origin", "--quiet")
	if err := cmd.Run(); err != nil {
		log.WarningLog.Printf("Failed to fetch from origin: %v", err)
		return
	}

	// Get the main branch name
	mainBranch := getMainBranch(gitRoot)

	// Get current commit count
	currentCount, err := getCommitCount(gitRoot, "HEAD")
	if err != nil {
		log.WarningLog.Printf("Failed to get current commit count: %v", err)
		return
	}

	// Get remote commit count
	remoteCount, err := getCommitCount(gitRoot, "origin/"+mainBranch)
	if err != nil {
		log.WarningLog.Printf("Failed to get remote commit count: %v", err)
		return
	}

	// Update the state
	uc.mu.Lock()
	defer uc.mu.Unlock()

	uc.lastCheck = time.Now()
	uc.currentCommitCount = currentCount
	uc.remoteCommitCount = remoteCount
	uc.updateAvailable = remoteCount > currentCount

	if uc.updateAvailable {
		log.InfoLog.Printf("Update available: %d commits behind", remoteCount-currentCount)
	}
}

// findGitRoot searches for a .git directory starting from the given path and going up
func findGitRoot(startPath string) string {
	current := startPath
	for current != "/" && current != "" {
		cmd := exec.Command("git", "-C", current, "rev-parse", "--show-toplevel")
		output, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output))
		}

		// Go up one directory
		if idx := strings.LastIndex(current, "/"); idx > 0 {
			current = current[:idx]
		} else {
			break
		}
	}
	return ""
}

// getMainBranch determines the main branch name for the repository
func getMainBranch(gitRoot string) string {
	// Try to get the default branch from remote
	cmd := exec.Command("sh", "-c", "git -C "+gitRoot+" remote show origin | sed -n '/HEAD branch/s/.*: //p'")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		return strings.TrimSpace(string(output))
	}

	// Fallback to common defaults
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "-C", gitRoot, "rev-parse", "--verify", "origin/"+branch)
		if err := cmd.Run(); err == nil {
			return branch
		}
	}

	return "main"
}

// getCommitCount returns the number of commits for a given ref
func getCommitCount(gitRoot, ref string) (int, error) {
	cmd := exec.Command("git", "-C", gitRoot, "rev-list", "--count", ref)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	count := 0
	if _, err := strings.NewReader(strings.TrimSpace(string(output))).Read([]byte{}); err == nil {
		// Parse the count
		if n, _ := strings.CutSuffix(strings.TrimSpace(string(output)), "\n"); n != "" {
			// Simple atoi implementation
			for _, c := range n {
				if c >= '0' && c <= '9' {
					count = count*10 + int(c-'0')
				}
			}
		}
	}

	return count, nil
}

// isAISquadRepo checks if the given directory is a ai-squad repository
func isAISquadRepo(gitRoot string) bool {
	// Check for ai-squad specific files
	requiredFiles := []string{
		"main.go",
		"go.mod",
		"app/app.go",
	}

	for _, file := range requiredFiles {
		if _, err := os.Stat(gitRoot + "/" + file); os.IsNotExist(err) {
			return false
		}
	}

	// Additionally check if go.mod contains ai-squad
	goModContent, err := os.ReadFile(gitRoot + "/go.mod")
	if err != nil {
		return false
	}

	return strings.Contains(string(goModContent), "ai-squad")
}
