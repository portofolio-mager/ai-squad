package overlay

import (
	"ai-squad/session/git"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NavigationView represents a single view in bookmark navigation
type NavigationView struct {
	Type        string // "current", "recent_commits", "bookmark", "initial"
	Title       string // Display title for this view
	Description string // Description line
	FromCommit  string // Starting commit (empty for initial)
	ToCommit    string // Ending commit ("HEAD" for current changes)
}

// GitStatusOverlay represents a git status overlay showing changed files
type GitStatusOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Callback function to be called when the overlay is dismissed
	OnDismiss func()
	// Files that have changed in this branch
	files []git.GitFileStatus
	// Branch name
	branchName string
	// Cached rendered content to prevent re-rendering
	cachedContent string

	// Bookmark mode settings
	bookmarkMode    bool
	bookmarks       []string         // List of bookmark commit SHAs (oldest to newest)
	currentView     int              // Index in navigation views (0 = most recent)
	navigationViews []NavigationView // Ordered list of views from most recent to oldest
	worktree        *git.GitWorktree // Reference to git worktree for bookmark navigation

	width  int
	height int
}

// NewGitStatusOverlay creates a new git status overlay
func NewGitStatusOverlay(branchName string, files []git.GitFileStatus) *GitStatusOverlay {
	return &GitStatusOverlay{
		Dismissed:    false,
		files:        files,
		branchName:   branchName,
		bookmarkMode: false,
		width:        80,
		height:       20,
	}
}

// NewGitStatusOverlayBookmarkMode creates a new git status overlay in bookmark mode
func NewGitStatusOverlayBookmarkMode(branchName string, worktree *git.GitWorktree) (*GitStatusOverlay, error) {
	// Get all bookmarks (oldest to newest)
	bookmarks, err := worktree.GetAllBookmarkCommits()
	if err != nil {
		return nil, fmt.Errorf("failed to get bookmarks: %w", err)
	}

	if len(bookmarks) == 0 {
		return nil, fmt.Errorf("no bookmarks found in this branch")
	}

	// Build navigation views from most recent to oldest
	var navigationViews []NavigationView

	// 1. Check for unstaged/uncommitted changes (most recent)
	lastBookmark := bookmarks[len(bookmarks)-1]
	currentChanges, err := worktree.GetChangedFilesSinceCommit(lastBookmark)
	if err == nil && len(currentChanges) > 0 {
		navigationViews = append(navigationViews, NavigationView{
			Type:        "current",
			Title:       "Current Changes",
			Description: "Uncommitted changes since last bookmark",
			FromCommit:  lastBookmark,
			ToCommit:    "HEAD",
		})
	}

	// 2. Latest commits before most recent bookmark (if more than one bookmark)
	if len(bookmarks) > 1 {
		secondLastBookmark := bookmarks[len(bookmarks)-2]
		navigationViews = append(navigationViews, NavigationView{
			Type:        "recent_commits",
			Title:       "Recent Changes",
			Description: "Changes in most recent bookmark period",
			FromCommit:  secondLastBookmark,
			ToCommit:    lastBookmark,
		})
	}

	// 3. Bookmark to bookmark (newer to older)
	for i := len(bookmarks) - 2; i >= 1; i-- {
		currentBookmark := bookmarks[i]
		prevBookmark := bookmarks[i-1]

		// Get bookmark message for title
		commitMsg, err := worktree.GetCommitMessage(currentBookmark)
		if err != nil {
			commitMsg = "Unknown bookmark"
		}
		bookmarkTitle := strings.TrimPrefix(commitMsg, "[BOOKMARK] ")

		navigationViews = append(navigationViews, NavigationView{
			Type:        "bookmark",
			Title:       fmt.Sprintf("Bookmark %d/%d - %s", i+1, len(bookmarks), bookmarkTitle),
			Description: fmt.Sprintf("Changes since previous bookmark"),
			FromCommit:  prevBookmark,
			ToCommit:    currentBookmark,
		})
	}

	// 4. First bookmark to branch creation (oldest)
	if len(bookmarks) > 0 {
		firstBookmark := bookmarks[0]
		commitMsg, err := worktree.GetCommitMessage(firstBookmark)
		if err != nil {
			commitMsg = "Unknown bookmark"
		}
		bookmarkTitle := strings.TrimPrefix(commitMsg, "[BOOKMARK] ")

		navigationViews = append(navigationViews, NavigationView{
			Type:        "initial",
			Title:       fmt.Sprintf("Initial Bookmark - %s", bookmarkTitle),
			Description: "Changes since branch creation",
			FromCommit:  "", // Empty means from start
			ToCommit:    firstBookmark,
		})
	}

	overlay := &GitStatusOverlay{
		Dismissed:       false,
		branchName:      branchName,
		bookmarkMode:    true,
		bookmarks:       bookmarks,
		currentView:     0, // Start with most recent (index 0)
		navigationViews: navigationViews,
		worktree:        worktree,
		width:           80,
		height:          20,
	}

	// Load files for the current view
	if err := overlay.loadViewFiles(); err != nil {
		return nil, fmt.Errorf("failed to load view files: %w", err)
	}

	return overlay, nil
}

// HandleKeyPress processes a key press and updates the state
// Returns true if the overlay should be closed
func (g *GitStatusOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	if g.bookmarkMode {
		switch msg.String() {
		case "left":
			return g.navigateView(1) // Go older (higher index)
		case "right":
			return g.navigateView(-1) // Go newer (lower index)
		}
	}

	// For normal mode, or any other key in bookmark mode, close the overlay.
	g.Dismissed = true
	if g.OnDismiss != nil {
		g.OnDismiss()
	}
	return true
}

// navigateView moves to the next/previous view in the navigation list
// Returns false to indicate the overlay should stay open
func (g *GitStatusOverlay) navigateView(direction int) bool {
	if !g.bookmarkMode || len(g.navigationViews) == 0 {
		return false
	}

	newIndex := g.currentView + direction

	// Check bounds
	if newIndex < 0 || newIndex >= len(g.navigationViews) {
		// Don't navigate beyond bounds
		return false
	}

	g.currentView = newIndex
	g.cachedContent = "" // Clear cache to force re-render

	// Load files for the new view
	if err := g.loadViewFiles(); err != nil {
		// On error, just stay at current position
		return false
	}

	return false // Keep overlay open
}

// loadViewFiles loads the files for the current navigation view
func (g *GitStatusOverlay) loadViewFiles() error {
	if !g.bookmarkMode || g.currentView < 0 || g.currentView >= len(g.navigationViews) {
		return fmt.Errorf("invalid view index")
	}

	view := g.navigationViews[g.currentView]
	var files []git.GitFileStatus
	var err error

	switch view.Type {
	case "current":
		// Current uncommitted changes since last bookmark
		files, err = g.worktree.GetChangedFilesSinceCommit(view.FromCommit)
	case "recent_commits":
		// Changes between two bookmark commits
		files, err = g.worktree.GetChangedFilesBetweenCommits(view.FromCommit, view.ToCommit)
	case "bookmark":
		// Changes between two bookmark commits
		files, err = g.worktree.GetChangedFilesBetweenCommits(view.FromCommit, view.ToCommit)
	case "initial":
		// Changes from branch start to first bookmark
		files, err = g.worktree.GetChangedFilesBetweenCommits(view.FromCommit, view.ToCommit)
	default:
		return fmt.Errorf("unknown view type: %s", view.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to load files for view %s: %w", view.Type, err)
	}

	g.files = files
	return nil
}

// Render renders the git status overlay
func (g *GitStatusOverlay) Render() string {
	// Return cached content if already rendered
	if g.cachedContent != "" {
		return g.cachedContent
	}

	// Create the content
	var content strings.Builder

	// Title
	var title string
	if g.bookmarkMode && g.currentView >= 0 && g.currentView < len(g.navigationViews) {
		// Use NavigationView data
		view := g.navigationViews[g.currentView]
		title = view.Title
		content.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
		content.WriteString("\n")
		content.WriteString(lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("Branch: %s | %s", g.branchName, view.Description)))
		content.WriteString("\n\n")
	} else {
		title = fmt.Sprintf("Git Status - Branch: %s", g.branchName)
		content.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
		content.WriteString("\n\n")
	}

	if len(g.files) == 0 {
		content.WriteString("No files changed in this branch.")
	} else {
		// Group files by status
		statusGroups := make(map[string][]string)
		statusNames := map[string]string{
			"M": "Modified",
			"A": "Added",
			"D": "Deleted",
			"R": "Renamed",
			"C": "Copied",
		}

		for _, file := range g.files {
			status := file.Status
			if len(status) > 1 {
				status = string(status[0]) // Take first character for complex statuses
			}
			statusGroups[status] = append(statusGroups[status], file.Path)
		}

		// Extract rendering logic into a helper function to avoid duplication
		renderGroup := func(status string, files []string) {
			statusName := statusNames[status]
			if statusName == "" {
				statusName = status
			}

			// Color code the status
			var statusStyle lipgloss.Style
			switch status {
			case "M":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow
			case "A":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // Green
			case "D":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // Red
			default:
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // Cyan
			}

			content.WriteString(statusStyle.Render(fmt.Sprintf("● %s (%s):", statusName, status)))
			content.WriteString("\n")

			for _, file := range files {
				content.WriteString(fmt.Sprintf("  %s", file))
				content.WriteString("\n")
			}
			content.WriteString("\n")
		}

		// Display files grouped by status in preferred order
		statusOrder := []string{"A", "M", "D", "R", "C"} // Added, Modified, Deleted, Renamed, Copied
		for _, status := range statusOrder {
			if files, ok := statusGroups[status]; ok {
				renderGroup(status, files)
				delete(statusGroups, status)
			}
		}

		// Handle any remaining statuses not in the predefined order
		// Sort remaining keys for consistent display order
		var remainingKeys []string
		for k := range statusGroups {
			remainingKeys = append(remainingKeys, k)
		}
		sort.Strings(remainingKeys)

		for _, status := range remainingKeys {
			renderGroup(status, statusGroups[status])
		}

		// Summary
		content.WriteString(fmt.Sprintf("Total: %d files changed", len(g.files)))
	}

	content.WriteString("\n\n")
	if g.bookmarkMode {
		// Show navigation message based on current position
		var navMsg string
		if len(g.navigationViews) <= 1 {
			navMsg = "Only one view available | Any other key to close"
		} else {
			// Show navigation hints based on position
			leftHint := "←"
			rightHint := "→"
			if g.currentView == 0 {
				leftHint = "" // Can't go newer
			}
			if g.currentView >= len(g.navigationViews)-1 {
				rightHint = "" // Can't go older
			}

			navMsg = fmt.Sprintf("%s%s Navigate (left=older, right=newer) | Any other key to close", leftHint, rightHint)
			if leftHint == "" && rightHint == "" {
				navMsg = "No more views to navigate | Any other key to close"
			}
		}
		content.WriteString(lipgloss.NewStyle().Faint(true).Render(navMsg))
	} else {
		content.WriteString(lipgloss.NewStyle().Faint(true).Render("Press any key to close"))
	}

	// Create styles
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(g.width).
		Height(g.height)

	// Apply the border style and cache the result
	g.cachedContent = style.Render(content.String())
	return g.cachedContent
}

// SetSize sets the size of the overlay
func (g *GitStatusOverlay) SetSize(width, height int) {
	g.width = width
	g.height = height
}
