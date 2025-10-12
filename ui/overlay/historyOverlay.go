package overlay

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HistoryOverlay represents a scrollable history view overlay
type HistoryOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Callback function to be called when the overlay is dismissed
	OnDismiss func()
	// Title of the overlay
	title string
	// Viewport for scrollable content
	viewport viewport.Model
	// Dimensions
	width  int
	height int
	// Help text shown at the bottom
	helpText string
}

// NewHistoryOverlay creates a new history overlay with the given title and content
func NewHistoryOverlay(title string, content string) *HistoryOverlay {
	h := &HistoryOverlay{
		Dismissed: false,
		title:     title,
		viewport:  viewport.New(0, 0),
		helpText:  "↑/↓ to scroll • ESC to close",
	}
	h.viewport.SetContent(content)
	return h
}

// SetSize updates the dimensions of the overlay
func (h *HistoryOverlay) SetSize(width, height int) {
	h.width = width
	h.height = height

	// Calculate viewport dimensions (account for borders, padding, title, and help text)
	// Border: 2 lines (top/bottom), padding: 2 lines, title: 2 lines, help: 2 lines
	viewportHeight := height - 8
	viewportWidth := width - 4 // Border and padding on sides

	if viewportHeight < 1 {
		viewportHeight = 1
	}
	if viewportWidth < 1 {
		viewportWidth = 1
	}

	h.viewport.Width = viewportWidth
	h.viewport.Height = viewportHeight

	// After setting dimensions, position at bottom to show most recent content
	h.viewport.GotoBottom()
}

// HandleKeyPress processes a key press and updates the state
// Returns true if the overlay should be closed
func (h *HistoryOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		h.Dismissed = true
		if h.OnDismiss != nil {
			h.OnDismiss()
		}
		return true
	case "up", "k":
		h.viewport.LineUp(1)
	case "down", "j":
		h.viewport.LineDown(1)
	case "pgup":
		h.viewport.HalfViewUp()
	case "pgdown":
		h.viewport.HalfViewDown()
	case "home", "ctrl+home":
		h.viewport.GotoTop()
	case "end", "ctrl+end":
		h.viewport.GotoBottom()
	}
	return false
}

// Render renders the history overlay
func (h *HistoryOverlay) Render(opts ...WhitespaceOption) string {
	// Title style
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62")).
		MarginBottom(1)

	// Help text style
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(1)

	// Container style
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		Width(h.width - 2). // Account for terminal padding
		Height(h.height - 2)

	// Build the content
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		titleStyle.Render(h.title),
		h.viewport.View(),
		helpStyle.Render(h.helpText),
	)

	return containerStyle.Render(content)
}

// Update handles viewport updates
func (h *HistoryOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	h.viewport, cmd = h.viewport.Update(msg)
	return h, cmd
}

// ScrollPercentage returns the current scroll position as a percentage
func (h *HistoryOverlay) ScrollPercentage() float64 {
	return h.viewport.ScrollPercent()
}

// Init initializes the overlay (required for tea.Model interface)
func (h *HistoryOverlay) Init() tea.Cmd {
	return nil
}

// View returns the view string (required for tea.Model interface)
func (h *HistoryOverlay) View() string {
	return h.Render()
}
