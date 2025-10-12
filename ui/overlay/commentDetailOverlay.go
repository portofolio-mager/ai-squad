package overlay

import (
	"fmt"
	"strings"

	"ai-squad/session/git"
	"ai-squad/ui"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Define constants for component heights
const (
	borderHeight          = 2 // Top and bottom border
	paddingHeight         = 2 // Padding
	headerHeight          = 5 // Header (maximum height)
	helpHeight            = 2 // Help section
	borderAndPaddingWidth = 8 // Terminal padding (4) + border (2) + padding (2)
	TerminalPadding       = 4 // Terminal padding
)

// CommentDetailOverlay represents a scrollable view for displaying full PR comment content
type CommentDetailOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Callback function to be called when the overlay is dismissed
	OnDismiss func()
	// The comment being displayed
	comment *git.PRComment
	// Viewport for scrollable content
	viewport viewport.Model
	// Dimensions
	width  int
	height int
	// Whether the viewport is ready
	ready bool
}

// NewCommentDetailOverlay creates a new comment detail overlay
func NewCommentDetailOverlay(comment *git.PRComment) *CommentDetailOverlay {
	c := &CommentDetailOverlay{
		Dismissed: false,
		comment:   comment,
		viewport:  viewport.New(0, 0),
		ready:     false,
	}
	return c
}

// SetSize updates the dimensions of the overlay
func (c *CommentDetailOverlay) SetSize(width, height int) {
	c.width = width
	c.height = height

	// Calculate total height of non-viewport components
	totalNonViewportHeight := borderHeight + paddingHeight + headerHeight + helpHeight

	// Calculate viewport dimensions
	viewportHeight := height - totalNonViewportHeight
	viewportWidth := width - borderAndPaddingWidth

	if viewportHeight < 1 {
		viewportHeight = 1
	}
	if viewportWidth < 1 {
		viewportWidth = 1
	}

	c.viewport.Width = viewportWidth
	c.viewport.Height = viewportHeight
	c.ready = true

	// Set the content
	c.updateContent()
}

// updateContent updates the viewport content with the comment details
func (c *CommentDetailOverlay) updateContent() {
	if c.comment == nil {
		c.viewport.SetContent("No comment to display")
		return
	}

	// Build the full comment content
	var content strings.Builder

	// Use lightweight markdown rendering for better performance
	renderedBody := ui.RenderMarkdownLight(c.comment.Body)
	content.WriteString(renderedBody)

	// Add additional metadata if available
	if c.comment.Path != "" {
		content.WriteString(fmt.Sprintf("\n\n── File Context ──\n%s", c.comment.Path))
		if c.comment.Line > 0 {
			content.WriteString(fmt.Sprintf(":%d", c.comment.Line))
		}
	}

	if c.comment.CommitID != "" {
		content.WriteString(fmt.Sprintf("\n\nCommit: %.7s", c.comment.CommitID))
	}

	content.WriteString(fmt.Sprintf("\n\nCreated: %s", c.comment.CreatedAt.Format("2006-01-02 15:04:05")))
	if c.comment.UpdatedAt != c.comment.CreatedAt {
		content.WriteString(fmt.Sprintf("\nUpdated: %s", c.comment.UpdatedAt.Format("2006-01-02 15:04:05")))
	}

	c.viewport.SetContent(content.String())
}

// HandleKeyPress processes a key press and updates the state
// Returns true if the overlay should be closed
func (c *CommentDetailOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc", "e", "q":
		c.Dismissed = true
		if c.OnDismiss != nil {
			c.OnDismiss()
		}
		return true
	case "up", "k":
		c.viewport.LineUp(1)
	case "down", "j":
		c.viewport.LineDown(1)
	case "pgup", "shift+up":
		c.viewport.HalfViewUp()
	case "pgdown", "shift+down":
		c.viewport.HalfViewDown()
	case "home", "g":
		c.viewport.GotoTop()
	case "end", "G":
		c.viewport.GotoBottom()
	}
	return false
}

// Render renders the comment detail overlay
func (c *CommentDetailOverlay) Render(opts ...WhitespaceOption) string {
	if !c.ready {
		return "Loading..."
	}

	// Header style
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	// Type style
	typeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("28")).
		Italic(true)

	// Author style
	authorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	// Help text style
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// Container style
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1).
		Width(c.width - TerminalPadding). // Account for terminal padding
		Height(c.height - 2)

	// Build header
	typeDisplay := c.comment.Type
	switch c.comment.Type {
	case "review":
		typeDisplay = "PR Review"
	case "review_comment":
		typeDisplay = "Review Comment"
	case "issue_comment":
		typeDisplay = "General Comment"
	}

	status := ""
	if c.comment.Accepted {
		status = " [✓ Accepted]"
	}

	header := lipgloss.JoinVertical(
		lipgloss.Left,
		headerStyle.Render("Comment Details"+status),
		typeStyle.Render(typeDisplay)+" by "+authorStyle.Render("@"+c.comment.Author),
		"",
	)

	// Scroll indicator
	scrollInfo := ""
	if c.viewport.TotalLineCount() > c.viewport.Height {
		scrollPercent := int(c.viewport.ScrollPercent() * 100)
		scrollInfo = fmt.Sprintf(" %d%%", scrollPercent)
		if c.viewport.AtTop() {
			scrollInfo += " ↓"
		} else if c.viewport.AtBottom() {
			scrollInfo += " ↑"
		} else {
			scrollInfo += " ↕"
		}
	}

	// Build the content
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		c.viewport.View(),
		"",
		helpStyle.Render("↑/↓ to scroll • ESC/e/q to close"+scrollInfo),
	)

	return containerStyle.Render(content)
}

// Update handles viewport updates
func (c *CommentDetailOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.SetSize(msg.Width, msg.Height)
		return c, nil
	case tea.KeyMsg:
		if c.HandleKeyPress(msg) {
			return c, nil
		}
	}

	var cmd tea.Cmd
	c.viewport, cmd = c.viewport.Update(msg)
	return c, cmd
}

// Init initializes the overlay (required for tea.Model interface)
func (c *CommentDetailOverlay) Init() tea.Cmd {
	return nil
}

// View returns the view string (required for tea.Model interface)
func (c *CommentDetailOverlay) View() string {
	return c.Render()
}
