package overlay

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TextOverlay represents a text screen overlay
type TextOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Callback function to be called when the overlay is dismissed
	OnDismiss func()
	// Content to display in the overlay
	content string
	// Viewport for scrollable content
	viewport viewport.Model
	// Dimensions
	width  int
	height int
	// Whether scrolling is needed
	needsScrolling bool
}

// NewTextOverlay creates a new text screen overlay with the given title and content
func NewTextOverlay(content string) *TextOverlay {
	t := &TextOverlay{
		Dismissed: false,
		content:   content,
		viewport:  viewport.New(0, 0),
	}
	t.viewport.SetContent(content)
	return t
}

// HandleKeyPress processes a key press and updates the state
// Returns true if the overlay should be closed
func (t *TextOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	// If scrolling is needed, handle navigation keys
	if t.needsScrolling {
		switch msg.String() {
		case "up", "k":
			t.viewport.LineUp(1)
			return false
		case "down", "j":
			t.viewport.LineDown(1)
			return false
		case "pgup":
			t.viewport.HalfViewUp()
			return false
		case "pgdown":
			t.viewport.HalfViewDown()
			return false
		case "home", "ctrl+home", "g":
			t.viewport.GotoTop()
			return false
		case "end", "ctrl+end", "G":
			t.viewport.GotoBottom()
			return false
		}
	}

	// Close on any other key
	t.Dismissed = true
	// Call the OnDismiss callback if it exists
	if t.OnDismiss != nil {
		t.OnDismiss()
	}
	return true
}

// Render renders the text overlay
func (t *TextOverlay) Render(opts ...WhitespaceOption) string {
	// Create styles
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2)

	var content string
	if t.needsScrolling {
		// Use viewport for scrollable content
		content = t.viewport.View()

		// Add scroll indicator at bottom if needed
		if t.viewport.TotalLineCount() > t.viewport.Height {
			scrollInfo := lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Render("↑/↓ to scroll • Press any other key to close")
			content = lipgloss.JoinVertical(lipgloss.Left, content, "", scrollInfo)
		}
	} else {
		// Use regular content for non-scrollable
		content = t.content
	}

	// Apply width if set
	if t.width > 0 {
		style = style.Width(t.width)
	}

	// Apply the border style and return
	return style.Render(content)
}

func (t *TextOverlay) SetWidth(width int) {
	t.width = width
	t.updateViewport()
}

// SetSize updates the dimensions of the overlay
func (t *TextOverlay) SetSize(width, height int) {
	t.width = width
	t.height = height
	t.updateViewport()
}

// updateViewport updates the viewport dimensions based on the overlay size
func (t *TextOverlay) updateViewport() {
	if t.height == 0 || t.width == 0 {
		return
	}

	// Calculate viewport dimensions (account for borders, padding, and scroll info)
	// Vertical overhead: 2 (border) + 2 (padding) + 2 (scroll info) = 6 lines
	viewportHeight := t.height - 6
	viewportWidth := t.width - 6 // Border and padding on sides

	if viewportHeight < 1 {
		viewportHeight = 1
	}
	if viewportWidth < 1 {
		viewportWidth = 1
	}

	t.viewport.Width = viewportWidth
	t.viewport.Height = viewportHeight

	// Determine if scrolling is needed
	totalLines := lipgloss.Height(t.content)
	t.needsScrolling = totalLines > viewportHeight
}
