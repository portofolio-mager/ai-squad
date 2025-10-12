package ui

import (
	"ai-squad/log"
	"ai-squad/session"

	"github.com/charmbracelet/lipgloss"
)

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┘", " ", "└")
	highlightColor    = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	inactiveTabStyle  = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(highlightColor).
				AlignHorizontal(lipgloss.Center)
	activeTabStyle = inactiveTabStyle.
			Border(activeTabBorder, true).
			AlignHorizontal(lipgloss.Center)
	windowStyle = lipgloss.NewStyle().
			BorderForeground(highlightColor).
			Border(lipgloss.NormalBorder(), false, true, true, true)
)

const (
	AITab = iota
	DiffTab
	TerminalTab
	JestTab
)

type Tab struct {
	Name   string
	Render func(width int, height int) string
}

// TabbedWindow has tabs at the top of a pane which can be selected. The tabs
// take up one rune of height.
type TabbedWindow struct {
	tabs []string

	activeTab      int
	height         int
	width          int
	verticalLayout bool

	preview  *PreviewPane
	diff     *DiffPane
	instance *session.Instance
	terminal *TerminalPane
	jest     *JestPane
}

func NewTabbedWindow(preview *PreviewPane, diff *DiffPane, terminal *TerminalPane, jest *JestPane) *TabbedWindow {
	return &TabbedWindow{
		tabs: []string{
			"AI",
			"Diff",
			"Terminal",
			"Jest",
		},
		preview:        preview,
		diff:           diff,
		terminal:       terminal,
		jest:           jest,
		verticalLayout: false,
	}
}

func (w *TabbedWindow) SetInstance(instance *session.Instance) {
	w.instance = instance
	// Update Jest pane with the current instance
	w.jest.SetInstance(instance)
}

// AdjustPreviewWidth adjusts the width of the preview pane to be 90% of the provided width.
func AdjustPreviewWidth(width int) int {
	return int(float64(width) * 0.9)
}

func (w *TabbedWindow) SetSize(width, height int) {
	// In vertical layout mode, use full width to match the list above
	if w.verticalLayout {
		w.width = width
	} else {
		w.width = AdjustPreviewWidth(width)
	}
	w.height = height

	// Calculate the content height by subtracting:
	// 1. Tab height (including border and padding)
	// 2. Window style vertical frame size
	// 3. Additional padding/spacing (2 for the newline and spacing)
	tabHeight := activeTabStyle.GetVerticalFrameSize() + 1
	contentHeight := height - tabHeight - windowStyle.GetVerticalFrameSize() - 2
	contentWidth := w.width - windowStyle.GetHorizontalFrameSize()

	w.preview.SetSize(contentWidth, contentHeight)
	w.diff.SetSize(contentWidth, contentHeight)
	w.terminal.SetSize(contentWidth, contentHeight)
	w.jest.SetSize(contentWidth, contentHeight)
}

func (w *TabbedWindow) GetPreviewSize() (width, height int) {
	return w.preview.width, w.preview.height
}

// SetVerticalLayout enables or disables vertical layout mode
func (w *TabbedWindow) SetVerticalLayout(vertical bool) {
	w.verticalLayout = vertical
}

// GetPreviewPane returns the preview pane
func (w *TabbedWindow) GetPreviewPane() *PreviewPane {
	return w.preview
}

func (w *TabbedWindow) Toggle() {
	w.cycleTabs(1)
}

// ToggleReverse cycles through tabs in reverse order
func (w *TabbedWindow) ToggleReverse() {
	w.cycleTabs(-1)
}

// cycleTabs handles cycling through tabs in a given direction.
func (w *TabbedWindow) cycleTabs(direction int) {
	if len(w.tabs) == 0 {
		return
	}
	numTabs := len(w.tabs)
	w.activeTab = (w.activeTab + direction + numTabs) % numTabs
}

// SetTab sets the active tab directly by index
func (w *TabbedWindow) SetTab(tabIndex int) {
	if tabIndex >= 0 && tabIndex < len(w.tabs) {
		w.activeTab = tabIndex
	}
}

// ToggleWithReset toggles the tab and resets preview pane to normal mode
func (w *TabbedWindow) ToggleWithReset(instance *session.Instance) error {
	// Reset preview pane to normal mode before switching
	if err := w.preview.ResetToNormalMode(instance); err != nil {
		return err
	}
	// Reset terminal pane to normal mode before switching
	if err := w.terminal.ResetToNormalMode(instance); err != nil {
		return err
	}
	w.activeTab = (w.activeTab + 1) % len(w.tabs)
	return nil
}

// UpdatePreview updates the content of the AI pane. instance may be nil.
func (w *TabbedWindow) UpdatePreview(instance *session.Instance) error {
	if w.activeTab != AITab {
		return nil
	}
	return w.preview.UpdateContent(instance)
}

func (w *TabbedWindow) UpdateDiff(instance *session.Instance) {
	if w.activeTab != DiffTab {
		return
	}
	w.diff.SetDiff(instance)
}

// ResetPreviewToNormalMode resets the preview pane to normal mode
func (w *TabbedWindow) ResetPreviewToNormalMode(instance *session.Instance) error {
	return w.preview.ResetToNormalMode(instance)
}

// ResetTerminalToNormalMode resets the terminal pane to normal mode
func (w *TabbedWindow) ResetTerminalToNormalMode(instance *session.Instance) error {
	return w.terminal.ResetToNormalMode(instance)
}

func (w *TabbedWindow) UpdateTerminal(instance *session.Instance) {
	if w.activeTab != TerminalTab {
		return
	}
	w.terminal.UpdateContent(instance)
}

// Add these new methods for handling scroll events
func (w *TabbedWindow) ScrollUp() {
	switch w.activeTab {
	case AITab:
		err := w.preview.ScrollUp(w.instance)
		if err != nil {
			log.InfoLog.Printf("tabbed window failed to scroll up: %v", err)
		}
	case DiffTab:
		w.diff.ScrollUp()
	case TerminalTab:
		err := w.terminal.ScrollUp(w.instance)
		if err != nil {
			log.InfoLog.Printf("terminal pane failed to scroll up: %v", err)
		}
	case JestTab:
		w.jest.ScrollUp()
	}
}

func (w *TabbedWindow) ScrollDown() {
	switch w.activeTab {
	case AITab:
		err := w.preview.ScrollDown(w.instance)
		if err != nil {
			log.InfoLog.Printf("tabbed window failed to scroll down: %v", err)
		}
	case DiffTab:
		w.diff.ScrollDown()
	case TerminalTab:
		err := w.terminal.ScrollDown(w.instance)
		if err != nil {
			log.InfoLog.Printf("terminal pane failed to scroll down: %v", err)
		}
	case JestTab:
		w.jest.ScrollDown()
	}
}

func (w *TabbedWindow) ScrollToTop() {
	if w.activeTab == DiffTab {
		w.diff.ScrollToTop()
	}
}

func (w *TabbedWindow) ScrollToBottom() {
	if w.activeTab == DiffTab {
		w.diff.ScrollToBottom()
	}
}

func (w *TabbedWindow) PageUp() {
	if w.activeTab == DiffTab {
		w.diff.PageUp()
	}
}

func (w *TabbedWindow) PageDown() {
	if w.activeTab == DiffTab {
		w.diff.PageDown()
	}
}

func (w *TabbedWindow) JumpToNextFile() {
	if w.activeTab == DiffTab {
		w.diff.JumpToNextFile()
	}
}

func (w *TabbedWindow) JumpToPrevFile() {
	if w.activeTab == DiffTab {
		w.diff.JumpToPrevFile()
	}
}

// IsInDiffTab returns true if the diff tab is currently active
func (w *TabbedWindow) IsInDiffTab() bool {
	return w.activeTab == DiffTab
}

// IsPreviewInScrollMode returns true if the preview pane is in scroll mode
func (w *TabbedWindow) IsPreviewInScrollMode() bool {
	return w.preview.isScrolling
}

// IsTerminalInScrollMode returns true if the terminal pane is in scroll mode
func (w *TabbedWindow) IsTerminalInScrollMode() bool {
	return w.terminal.isScrolling
}

// IsInAITab returns true if the AI tab is currently active
func (w *TabbedWindow) IsInAITab() bool {
	return w.activeTab == AITab
}

// IsInTerminalTab returns true if the terminal tab is currently active
func (w *TabbedWindow) IsInTerminalTab() bool {
	return w.activeTab == TerminalTab
}

// IsInJestTab returns true if the Jest tab is currently active
func (w *TabbedWindow) IsInJestTab() bool {
	return w.activeTab == JestTab
}

// UpdateJest updates the Jest pane with test results
func (w *TabbedWindow) UpdateJest(instance *session.Instance) {
	if w.activeTab != JestTab {
		return
	}
	w.jest.RunTests(instance)
}

// JestRerunTests reruns the Jest tests
func (w *TabbedWindow) JestRerunTests() {
	if w.activeTab == JestTab && w.instance != nil {
		w.jest.RunTests(w.instance)
	}
}

// SetDiffModeAll sets the diff view to show all changes
func (w *TabbedWindow) SetDiffModeAll() {
	w.diff.SetDiffMode(DiffModeAll)
}

// SetDiffModeLastCommit sets the diff view to show only the last commit
func (w *TabbedWindow) SetDiffModeLastCommit() {
	w.diff.SetDiffMode(DiffModeLastCommit)
}

// NavigateToPrevCommit moves to the previous (older) commit in diff view
func (w *TabbedWindow) NavigateToPrevCommit() {
	if w.activeTab == DiffTab {
		w.diff.NavigateToPrevCommit()
	}
}

// NavigateToNextCommit moves to the next (newer) commit in diff view
func (w *TabbedWindow) NavigateToNextCommit() {
	if w.activeTab == DiffTab {
		w.diff.NavigateToNextCommit()
	}
}

// GetCurrentDiffFile returns the file path currently being viewed in the diff tab
func (w *TabbedWindow) GetCurrentDiffFile() string {
	if w.activeTab == DiffTab {
		return w.diff.GetCurrentFile()
	}
	return ""
}

func (w *TabbedWindow) String() string {
	if w.width == 0 || w.height == 0 {
		return ""
	}

	// In vertical layout mode, show simplified tab indicator
	if w.verticalLayout {
		var content string
		if w.activeTab == 0 {
			content = w.preview.String()
		} else {
			content = w.diff.String()
		}

		// Create a simple tab indicator for mobile mode
		tabIndicator := ""
		for i, tabName := range w.tabs {
			if i == w.activeTab {
				// Active tab with highlight
				tabIndicator += lipgloss.NewStyle().
					Foreground(highlightColor).
					Bold(true).
					Render("● " + tabName)
			} else {
				// Inactive tab
				tabIndicator += lipgloss.NewStyle().
					Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"}).
					Render("○ " + tabName)
			}
			if i < len(w.tabs)-1 {
				tabIndicator += "  "
			}
		}

		// Add tab switching hint
		tabIndicator += lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
			Render(" (Tab to switch)")

		// Simple border with tab indicator at top
		simpleWindowStyle := lipgloss.NewStyle().
			BorderForeground(highlightColor).
			Border(lipgloss.NormalBorder(), true, true, true, true).
			Width(w.width - 2).
			Height(w.height - 3) // Leave space for tab indicator

		// Combine tab indicator and content
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Padding(0, 1).Render(tabIndicator),
			simpleWindowStyle.Render(content),
		)
	}

	// Original horizontal layout with tabs
	var renderedTabs []string

	tabWidth := w.width / len(w.tabs)
	lastTabWidth := w.width - tabWidth*(len(w.tabs)-1)
	tabHeight := activeTabStyle.GetVerticalFrameSize() + 1 // get padding border margin size + 1 for character height

	for i, t := range w.tabs {
		width := tabWidth
		if i == len(w.tabs)-1 {
			width = lastTabWidth
		}

		var style lipgloss.Style
		isFirst, isLast, isActive := i == 0, i == len(w.tabs)-1, i == w.activeTab
		if isActive {
			style = activeTabStyle
		} else {
			style = inactiveTabStyle
		}
		border, _, _, _, _ := style.GetBorder()
		if isFirst && isActive {
			border.BottomLeft = "│"
		} else if isFirst {
			border.BottomLeft = "├"
		} else if isLast && isActive {
			border.BottomRight = "│"
		} else if isLast {
			border.BottomRight = "┤"
		}
		style = style.Border(border)
		style = style.Width(width - 1)
		renderedTabs = append(renderedTabs, style.Render(t))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	var content string
	switch w.activeTab {
	case AITab:
		content = w.preview.String()
	case DiffTab:
		content = w.diff.String()
	case TerminalTab:
		content = w.terminal.String()
	case JestTab:
		content = w.jest.String()
	}
	window := windowStyle.Render(
		lipgloss.Place(
			w.width, w.height-2-windowStyle.GetVerticalFrameSize()-tabHeight,
			lipgloss.Left, lipgloss.Top, content))

	return lipgloss.JoinVertical(lipgloss.Left, "\n", row, window)
}
