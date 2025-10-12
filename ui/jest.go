package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-squad/config"
	"ai-squad/log"
	"ai-squad/session"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

const (
	// maxAutoOpenFailedFiles is the maximum number of failed test files to open automatically
	maxAutoOpenFailedFiles = 5
	// ideOpenDelay is the delay between opening files in the IDE to avoid overwhelming it
	ideOpenDelay = 100 * time.Millisecond
)

type JestPane struct {
	width    int
	height   int
	viewport viewport.Model
	content  string
	// Per-instance state maps
	instanceStates  map[string]*JestInstanceState
	currentInstance *session.Instance
	mu              sync.Mutex
	globalConfig    *config.Config
}

type JestInstanceState struct {
	running      bool
	testResults  []TestResult
	failedFiles  []string
	workingDir   string
	currentIndex int
	liveOutput   string
	cmd          *exec.Cmd
	outputChan   chan string
}

type TestResult struct {
	FilePath    string
	TestName    string
	Status      string
	ErrorOutput string
	Line        int
}

func NewJestPane(globalConfig *config.Config) *JestPane {
	vp := viewport.New(0, 0)
	return &JestPane{
		viewport:       vp,
		instanceStates: make(map[string]*JestInstanceState),
		globalConfig:   globalConfig,
	}
}

func (j *JestPane) SetSize(width, height int) {
	j.width = width
	j.height = height
	j.updateViewport()
}

func (j *JestPane) getInstanceKey(instance *session.Instance) string {
	if instance == nil {
		return ""
	}
	// Always use a combination of Path and Branch as a stable unique key
	// This ensures consistency even if Title changes later
	return fmt.Sprintf("%s:%s", instance.Path, instance.Branch)
}

func (j *JestPane) getCurrentState() *JestInstanceState {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.currentInstance == nil {
		return nil
	}

	key := j.getInstanceKey(j.currentInstance)
	state, exists := j.instanceStates[key]
	if !exists {
		// Create a new state for this instance if it doesn't exist
		state = &JestInstanceState{
			testResults:  []TestResult{},
			failedFiles:  []string{},
			currentIndex: -1,
		}
		j.instanceStates[key] = state
	}
	return state
}

func (j *JestPane) getOrCreateState(instance *session.Instance) *JestInstanceState {
	j.mu.Lock()
	defer j.mu.Unlock()

	if instance == nil {
		return nil
	}

	key := j.getInstanceKey(instance)
	state, exists := j.instanceStates[key]
	if !exists {
		state = &JestInstanceState{
			testResults:  []TestResult{},
			failedFiles:  []string{},
			currentIndex: -1,
		}
		j.instanceStates[key] = state
	}
	return state
}

func (j *JestPane) SetInstance(instance *session.Instance) {
	j.mu.Lock()
	j.currentInstance = instance
	j.mu.Unlock()
	j.updateViewport()

	// If there's content, scroll to bottom
	state := j.getCurrentState()
	if state != nil && state.liveOutput != "" {
		j.viewport.GotoBottom()
	}
}

func (j *JestPane) String() string {
	if j.height < 5 {
		return ""
	}

	header := titleStyle.Render("Jest Test Runner")

	var status string
	var instanceInfo string
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	if j.currentInstance != nil {
		instanceInfo = fmt.Sprintf(" - %s", j.currentInstance.Title)
	}

	state := j.getCurrentState()
	if j.currentInstance == nil {
		status = statusStyle.Render("No instance selected")
	} else if state != nil && state.running {
		status = statusStyle.Render("⏳ Running tests...")
	} else if state != nil && len(state.failedFiles) > 0 {
		status = failureStyle.Render(fmt.Sprintf("❌ %d test(s) failed", len(state.failedFiles)))
	} else if state != nil && state.liveOutput != "" {
		status = statusStyle.Render("Test complete")
	} else {
		status = statusStyle.Render("No tests run yet")
	}

	// When tests are running, use viewport for auto-scrolling
	// When tests are not running, render as plain text for proper scrolling
	var content string
	if state != nil && state.running {
		content = j.viewport.View()
	} else {
		// Render content as plain text when not running
		rawContent := j.formatContent()
		// Calculate available height for content
		availableHeight := j.height - 4 // header + status + help + spacing

		if availableHeight > 0 {
			lines := strings.Split(rawContent, "\n")

			// Apply scrolling offset manually
			scrollOffset := j.viewport.YOffset
			if scrollOffset < 0 {
				scrollOffset = 0
			}

			visibleLines := []string{}
			for i := scrollOffset; i < len(lines) && i < scrollOffset+availableHeight; i++ {
				visibleLines = append(visibleLines, lines[i])
			}

			// Pad with empty lines if needed
			for len(visibleLines) < availableHeight {
				visibleLines = append(visibleLines, "")
			}

			content = strings.Join(visibleLines, "\n")
		} else {
			content = rawContent
		}
	}

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	help := helpStyle.Render("↑/↓: scroll • r: rerun tests • Auto-scrolls during test run")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header+instanceInfo,
		status,
		content,
		help,
	)
}

func (j *JestPane) formatContent() string {
	state := j.getCurrentState()
	if state == nil {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
		return dimStyle.Render("No instance selected")
	}

	// Always show raw output if available (whether running or not)
	if state.liveOutput != "" {
		return state.liveOutput
	}

	// If no output yet
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpText := `No test results to display.

Available commands:
  t - Run all tests
  r - Re-run tests (when in Jest tab)
  n - Navigate to next failure
  p - Navigate to previous failure
  ↵ - Open failed test in IDE`
	return dimStyle.Render(helpText)
}

func (j *JestPane) RunTests(instance *session.Instance) error {
	state := j.getOrCreateState(instance)
	if state == nil {
		return fmt.Errorf("no instance provided")
	}

	// Stop any existing test run for this instance
	j.stopTests(instance)

	// Reset state
	j.mu.Lock()
	state.running = true
	state.testResults = []TestResult{}
	state.failedFiles = []string{}
	state.currentIndex = -1
	state.liveOutput = ""
	j.mu.Unlock()

	// Reset scroll position when starting new test
	j.viewport.YOffset = 0

	// Get the git worktree path from the instance
	gitWorktree, err := instance.GetGitWorktree()
	if err != nil {
		j.mu.Lock()
		state.running = false
		state.liveOutput = errorStyle.Render(fmt.Sprintf("Error getting git worktree: %v", err))
		j.mu.Unlock()
		j.updateViewport()
		return err
	}

	worktreePath := gitWorktree.GetWorktreePath()

	// Find package.json in the worktree directory
	workDir, err := j.findJestWorkingDir(worktreePath)
	if err != nil {
		j.mu.Lock()
		state.running = false
		state.liveOutput = errorStyle.Render(fmt.Sprintf("Error finding package.json: %v", err))
		j.mu.Unlock()
		j.updateViewport()
		return err
	}

	state.workingDir = workDir

	// Create output channel for live updates
	outputChan := make(chan string, 100)
	state.outputChan = outputChan

	// Start goroutine to update live output
	go func() {
		for line := range outputChan {
			j.mu.Lock()
			state.liveOutput += line + "\n"
			j.mu.Unlock()
			j.updateViewport()
			// Auto-scroll to bottom
			j.viewport.GotoBottom()
		}
	}()

	// Run Jest with streaming output
	go j.runJestWithStream(instance, state, workDir, outputChan)

	return nil
}

// parseFailedTestFile extracts the file path from a FAIL line if present
func parseFailedTestFile(line string, workDir string) string {
	// Check if line starts with "FAIL "
	if !strings.HasPrefix(strings.TrimSpace(line), "FAIL ") {
		return ""
	}

	// Extract file path from FAIL line
	// Format: "FAIL src/pages/individualDashboard/component.test.js"
	trimmedLine := strings.TrimSpace(line)
	if len(trimmedLine) <= 5 { // "FAIL " is 5 characters
		return ""
	}

	filePath := strings.TrimSpace(trimmedLine[5:])
	// Remove any trailing whitespace or test duration info
	if idx := strings.IndexAny(filePath, " \t("); idx > 0 {
		filePath = filePath[:idx]
	}

	// Check if it's a valid test file
	if !strings.HasSuffix(filePath, ".js") && !strings.HasSuffix(filePath, ".jsx") &&
		!strings.HasSuffix(filePath, ".ts") && !strings.HasSuffix(filePath, ".tsx") &&
		!strings.HasSuffix(filePath, ".test.js") && !strings.HasSuffix(filePath, ".spec.js") {
		return ""
	}

	// Convert to absolute path if needed
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(workDir, filePath)
	}

	return absPath
}

func (j *JestPane) runJestWithStream(instance *session.Instance, state *JestInstanceState, workDir string, outputChan chan<- string) {
	defer close(outputChan)

	// Run Jest without JSON for live output
	cmd := exec.Command("yarn", "tester")
	cmd.Dir = workDir

	// Log debug info
	log.InfoLog.Printf("Running Jest tests - command: yarn tester, workDir: %s, instance path: %s", workDir, instance.Path)

	// Store cmd in state so we can kill it if needed
	j.mu.Lock()
	state.cmd = cmd
	j.mu.Unlock()

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		outputChan <- fmt.Sprintf("Error creating stdout pipe: %v", err)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		outputChan <- fmt.Sprintf("Error creating stderr pipe: %v", err)
		return
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		outputChan <- fmt.Sprintf("Error starting Jest: %v", err)
		j.mu.Lock()
		state.running = false
		j.mu.Unlock()
		return
	}

	// Collect all output for parsing
	var allOutput strings.Builder
	var failedFilesMu sync.Mutex
	failedFiles := []string{}

	// Helper function to add failed files with thread safety
	addFailedFile := func(file string) {
		failedFilesMu.Lock()
		defer failedFilesMu.Unlock()
		for _, f := range failedFiles {
			if f == file {
				return // Already added
			}
		}
		failedFiles = append(failedFiles, file)
	}

	// Create wait group for both readers
	var wg sync.WaitGroup
	wg.Add(2)

	// Read stdout in goroutine
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			outputChan <- line
			allOutput.WriteString(line + "\n")

			// Look for test failures in real-time
			if failedFile := parseFailedTestFile(line, workDir); failedFile != "" {
				addFailedFile(failedFile)
			}
		}
		if err := scanner.Err(); err != nil {
			outputChan <- fmt.Sprintf("Error reading stdout: %v", err)
		}
	}()

	// Read stderr in goroutine
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			outputChan <- line
			allOutput.WriteString(line + "\n")

			// Also check stderr for FAIL lines (Jest might output to stderr)
			if failedFile := parseFailedTestFile(line, workDir); failedFile != "" {
				addFailedFile(failedFile)
			}
		}
		if err := scanner.Err(); err != nil {
			outputChan <- fmt.Sprintf("Error reading stderr: %v", err)
		}
	}()

	// Wait for command to finish first
	cmdErr := cmd.Wait()

	// Then wait for readers to finish
	wg.Wait()

	if cmdErr != nil {
		outputChan <- fmt.Sprintf("\nCommand exited with error: %v", cmdErr)
	}

	// If we didn't get any output, try running with CombinedOutput as fallback
	if allOutput.Len() == 0 {
		outputChan <- "\nNo output captured from pipes, trying alternative method..."
		fallbackCmd := exec.Command("yarn", "tester")
		fallbackCmd.Dir = workDir
		fallbackOutput, fallbackErr := fallbackCmd.CombinedOutput()
		if fallbackErr != nil {
			outputChan <- fmt.Sprintf("\nFallback command error: %v", fallbackErr)
		}

		// Parse fallback output for failed files and send to UI
		lines := strings.Split(string(fallbackOutput), "\n")
		for _, line := range lines {
			if failedFile := parseFailedTestFile(line, workDir); failedFile != "" {
				addFailedFile(failedFile)
			}
			outputChan <- line
		}
	}

	// Auto-open failed files in IDE
	if len(failedFiles) > 0 {
		j.autoOpenFailedTests(failedFiles)
	}

	j.mu.Lock()
	state.running = false
	state.failedFiles = failedFiles
	state.cmd = nil
	// Keep the liveOutput so it persists after tests complete
	j.mu.Unlock()
	j.updateViewport()
	// Ensure we're scrolled to bottom to see final results
	j.viewport.GotoBottom()
}

func (j *JestPane) stopTests(instance *session.Instance) {
	state := j.getOrCreateState(instance)
	if state == nil || state.cmd == nil {
		return
	}

	j.mu.Lock()
	if state.cmd.Process != nil {
		state.cmd.Process.Kill()
	}
	if state.outputChan != nil {
		// Don't close the channel here as the goroutine will close it
		state.outputChan = nil
	}
	state.running = false
	j.mu.Unlock()
}

func (j *JestPane) autoOpenFailedTests(failedFiles []string) {
	state := j.getCurrentState()
	if state == nil {
		return
	}

	ideCmd := config.GetEffectiveIdeCommand(state.workingDir, j.globalConfig)
	if ideCmd == "" {
		return
	}

	// Open up to maxAutoOpenFailedFiles failed test files
	maxFiles := maxAutoOpenFailedFiles
	if len(failedFiles) < maxFiles {
		maxFiles = len(failedFiles)
	}

	for i := 0; i < maxFiles; i++ {
		// Log what we're opening
		log.InfoLog.Printf("Opening failed test file in IDE: %s", failedFiles[i])

		cmd := exec.Command(ideCmd, failedFiles[i])
		if err := cmd.Start(); err != nil {
			log.ErrorLog.Printf("Failed to open file in IDE: %s, error: %v", failedFiles[i], err)
		}
		// Small delay to avoid overwhelming the IDE
		time.Sleep(ideOpenDelay)
	}
}

func (j *JestPane) findJestWorkingDir(startPath string) (string, error) {
	// First check if startPath is a file or directory
	info, err := os.Stat(startPath)
	if err != nil {
		return "", err
	}

	var searchDir string
	if info.IsDir() {
		searchDir = startPath
	} else {
		searchDir = filepath.Dir(startPath)
	}

	// First, check if the starting directory itself has a package.json
	packagePath := filepath.Join(searchDir, "package.json")
	if _, err := os.Stat(packagePath); err == nil {
		// Found package.json in the starting directory, use it
		return searchDir, nil
	}

	// Search upward for package.json
	currentDir := searchDir
	for {
		packagePath := filepath.Join(currentDir, "package.json")
		if _, err := os.Stat(packagePath); err == nil {
			// Found a package.json, use this directory
			return currentDir, nil
		}

		// Move up one directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir || currentDir == "/" {
			break
		}
		currentDir = parentDir
	}

	// If not found upward, search downward from original directory
	if info.IsDir() {
		var foundDir string
		walkErr := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.ErrorLog.Printf("Error walking path %s: %v", path, err)
				return nil // Continue walking despite error
			}
			if info.Name() == "package.json" {
				foundDir = filepath.Dir(path)
				return io.EOF // Stop walking on first package.json found
			}
			return nil
		})

		// Check if walk ended with an error other than EOF (which we use to stop early)
		if walkErr != nil && walkErr != io.EOF {
			log.ErrorLog.Printf("Error walking directory tree: %v", walkErr)
		}

		if foundDir != "" {
			return foundDir, nil
		}
	}

	return "", fmt.Errorf("no package.json found in %s or parent directories", startPath)
}

func (j *JestPane) updateViewport() {
	// Ensure viewport has correct dimensions
	if j.viewport.Width != j.width {
		j.viewport.Width = j.width
	}
	expectedHeight := j.height - 4
	if j.viewport.Height != expectedHeight && expectedHeight > 0 {
		j.viewport.Height = expectedHeight
	}
	// Update content
	j.viewport.SetContent(j.formatContent())
}

func (j *JestPane) ScrollUp() {
	state := j.getCurrentState()
	// Only allow scrolling when tests are not running
	if state == nil || state.running {
		return
	}

	// Update viewport for scrolling
	j.updateViewport()

	// Update scroll position
	if j.viewport.YOffset > 0 {
		j.viewport.YOffset -= 3
		if j.viewport.YOffset < 0 {
			j.viewport.YOffset = 0
		}
	}
}

func (j *JestPane) ScrollDown() {
	state := j.getCurrentState()
	// Only allow scrolling when tests are not running
	if state == nil || state.running {
		return
	}

	// Update viewport for scrolling
	j.updateViewport()

	// Calculate total lines to determine scroll limits
	content := j.formatContent()
	totalLines := len(strings.Split(content, "\n"))
	availableHeight := j.height - 4

	// Update scroll position
	maxOffset := totalLines - availableHeight
	if maxOffset > 0 && j.viewport.YOffset < maxOffset {
		j.viewport.YOffset += 3
		if j.viewport.YOffset > maxOffset {
			j.viewport.YOffset = maxOffset
		}
	}
}

func (j *JestPane) ResetToNormalMode() {
	j.viewport.SetYOffset(0)
}

// Add styles used by Jest pane
var (
	fileHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("cyan")).
			MarginTop(1)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("white"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("red"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("green"))

	failureStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("red")).
			Bold(true)
)
