package app

import (
	"ai-squad/config"
	"ai-squad/log"

	tea "github.com/charmbracelet/bubbletea"
)

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	// Store window dimensions
	m.terminalWidth = msg.Width
	m.terminalHeight = msg.Height
	m.windowWidth = msg.Width
	m.windowHeight = msg.Height

	// Determine effective layout mode (always auto-detect)
	m.currentLayoutMode = m.appConfig.GetEffectiveLayoutMode(msg.Width)

	// Update menu mode based on layout
	m.menu.SetCompactMode(m.appConfig.ShouldUseCompactMenu(m.currentLayoutMode))
	m.menu.SetVerticalMode(m.currentLayoutMode == config.LayoutModeMobile)

	// Update logo visibility
	m.tabbedWindow.GetPreviewPane().SetHideLogo(m.appConfig.ShouldHideLogo(m.currentLayoutMode))

	if m.textInputOverlay != nil {
		m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}
	if m.textOverlay != nil {
		width, height := m.calculateOverlayDimensions()
		m.textOverlay.SetSize(width, height)
	}
	if m.historyOverlay != nil {
		m.historyOverlay.SetSize(int(float32(msg.Width)*0.9), int(float32(msg.Height)*0.9))
	}

	menuHeight := m.applyLayoutMode(msg.Width, msg.Height)

	// Update preview size for tmux sessions
	previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
	if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
		log.ErrorLog.Print(err)
	}
	m.menu.SetSize(msg.Width, menuHeight)
}

// applyLayoutMode applies the current layout mode to all UI components
// Returns the menu height for further use
func (m *home) applyLayoutMode(width, height int) int {
	var listWidth, tabsWidth, contentHeight, menuHeight int

	switch m.currentLayoutMode {
	case config.LayoutModeMobile:
		// Mobile: Vertical stacking layout
		// List takes full width but only 25% height (reduced from 30% to save space)
		// Preview/diff takes full width and remaining height
		// Menu gets a fixed compact height
		menuHeight = 2 // Fixed compact height for menu in mobile mode (reduced from 3)
		listHeight := int(float32(height) * 0.25)
		previewHeight := height - listHeight - menuHeight - 1

		// Ensure minimum heights for usability
		if listHeight < 3 {
			listHeight = 3
			previewHeight = height - listHeight - menuHeight - 1
		}
		if previewHeight < 5 {
			previewHeight = 5
			listHeight = height - previewHeight - menuHeight - 1
		}
		if menuHeight < 1 {
			menuHeight = 1
		}

		m.list.SetVerticalLayout(true)
		m.list.SetSize(width, listHeight)
		m.tabbedWindow.SetVerticalLayout(true)
		m.tabbedWindow.SetSize(width, previewHeight)

	default: // LayoutModeFull
		// Full: Standard 30/70 split
		listWidth = int(float32(width) * 0.3)
		tabsWidth = width - listWidth
		contentHeight = int(float32(height) * 0.9)
		menuHeight = height - contentHeight - 1

		// Ensure minimum widths for usability
		if listWidth < 20 {
			listWidth = 20
			tabsWidth = width - listWidth
		}
		if tabsWidth < 30 {
			tabsWidth = 30
			listWidth = width - tabsWidth
		}

		m.list.SetVerticalLayout(false)
		m.list.SetSize(listWidth, contentHeight)
		m.tabbedWindow.SetVerticalLayout(false)
		m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	}

	// Always set error box to 90% width
	m.errBox.SetSize(int(float32(width)*0.9), 1)

	// Adjust overlay sizes based on layout mode
	if m.textInputOverlay != nil {
		overlayWidth := int(float32(width) * 0.8)
		if m.currentLayoutMode == config.LayoutModeFull {
			overlayWidth = int(float32(width) * 0.6)
		}
		m.textInputOverlay.SetSize(overlayWidth, int(float32(height)*0.4))
	}
	if m.textOverlay != nil {
		overlayWidth := int(float32(width) * 0.8)
		if m.currentLayoutMode == config.LayoutModeFull {
			overlayWidth = int(float32(width) * 0.6)
		}
		m.textOverlay.SetWidth(overlayWidth)
	}

	return menuHeight
}

// handleTabSwitch handles tab switching in both forward and reverse directions
func (m *home) handleTabSwitch(reverse bool) (tea.Model, tea.Cmd) {
	if reverse {
		m.tabbedWindow.ToggleReverse()
	} else {
		m.tabbedWindow.Toggle()
	}
	m.menu.SetInDiffTab(m.tabbedWindow.IsInDiffTab())
	return m, m.instanceChanged()
}
