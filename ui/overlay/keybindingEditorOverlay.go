package overlay

import (
	"fmt"
	"strings"

	"ai-squad/keys"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type keybindingEditorMode int

const (
	modeList keybindingEditorMode = iota
	modeEditKeys
	modeConfirmSave
)

// KeybindingEditorOverlay represents the keybinding editor overlay
type KeybindingEditorOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Current keybindings configuration
	config *keys.KeyBindingsConfig
	// Currently selected binding index
	selectedIndex int
	// Current mode (list view or editing)
	mode keybindingEditorMode
	// Current editing state
	editingBinding *keys.KeyBinding
	editingKeys    []string
	captureNextKey bool
	// Dimensions
	width  int
	height int
	// Styles
	titleStyle    lipgloss.Style
	itemStyle     lipgloss.Style
	selectedStyle lipgloss.Style
	helpStyle     lipgloss.Style
	borderStyle   lipgloss.Style
}

// NewKeybindingEditorOverlay creates a new keybinding editor overlay
func NewKeybindingEditorOverlay() *KeybindingEditorOverlay {
	// Load current keybindings
	cfg, err := keys.LoadKeyBindings()
	if err != nil {
		cfg = keys.DefaultKeyBindings()
	}

	return &KeybindingEditorOverlay{
		Dismissed:     false,
		config:        cfg,
		selectedIndex: 0,
		mode:          modeList,
		width:         80,
		height:        30,
		titleStyle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
		itemStyle:     lipgloss.NewStyle().Padding(0, 2),
		selectedStyle: lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230")),
		helpStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		borderStyle:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")),
	}
}

// SetDimensions updates the overlay dimensions
func (k *KeybindingEditorOverlay) SetDimensions(width, height int) {
	k.width = width
	k.height = height
}

// HandleKeyPress processes a key press and updates the state
func (k *KeybindingEditorOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	switch k.mode {
	case modeList:
		return k.handleListMode(msg)
	case modeEditKeys:
		return k.handleEditMode(msg)
	case modeConfirmSave:
		return k.handleConfirmMode(msg)
	}
	return false
}

func (k *KeybindingEditorOverlay) handleListMode(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up", "k":
		if k.selectedIndex > 0 {
			k.selectedIndex--
		}
	case "down", "j":
		if k.selectedIndex < len(k.config.Bindings)-1 {
			k.selectedIndex++
		}
	case "enter", "e":
		// Enter edit mode for selected binding
		k.editingBinding = &k.config.Bindings[k.selectedIndex]
		k.editingKeys = make([]string, len(k.editingBinding.Keys))
		copy(k.editingKeys, k.editingBinding.Keys)
		k.mode = modeEditKeys
		k.captureNextKey = true
	case "s":
		// Save changes
		k.mode = modeConfirmSave
	case "r":
		// Reset to defaults
		k.config = keys.DefaultKeyBindings()
		k.selectedIndex = 0
	case "q", "esc":
		// Close without saving
		k.Dismissed = true
		return true
	}
	return false
}

func (k *KeybindingEditorOverlay) handleEditMode(msg tea.KeyMsg) bool {
	if k.captureNextKey {
		// Capture the key combination
		keyStr := msg.String()

		// Special handling for certain keys
		if keyStr == "esc" {
			// Cancel editing
			k.mode = modeList
			k.captureNextKey = false
			return false
		}

		// Add the key to the binding
		k.editingKeys = []string{keyStr}
		k.captureNextKey = false
		return false
	}

	switch msg.String() {
	case "enter":
		// Save the binding
		k.editingBinding.Keys = k.editingKeys
		k.config.Bindings[k.selectedIndex] = *k.editingBinding
		k.mode = modeList
	case "a":
		// Add another key
		k.captureNextKey = true
	case "d":
		// Delete last key
		if len(k.editingKeys) > 0 {
			k.editingKeys = k.editingKeys[:len(k.editingKeys)-1]
		}
	case "esc":
		// Cancel editing
		k.mode = modeList
	}
	return false
}

func (k *KeybindingEditorOverlay) handleConfirmMode(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "y":
		// Save configuration
		if err := k.config.Save(); err == nil {
			k.Dismissed = true
			return true
		}
		// Show error somehow
		k.mode = modeList
	case "n", "esc":
		// Cancel save
		k.mode = modeList
	}
	return false
}

// Render renders the overlay
func (k *KeybindingEditorOverlay) Render() string {
	var content string

	switch k.mode {
	case modeList:
		content = k.renderList()
	case modeEditKeys:
		content = k.renderEdit()
	case modeConfirmSave:
		content = k.renderConfirm()
	}

	// Apply border and center
	bordered := k.borderStyle.Render(content)
	return lipgloss.Place(k.width, k.height, lipgloss.Center, lipgloss.Center, bordered)
}

func (k *KeybindingEditorOverlay) renderList() string {
	var lines []string

	// Title
	lines = append(lines, k.titleStyle.Render("Keyboard Configuration"))
	lines = append(lines, "")

	// Headers
	headers := fmt.Sprintf("%-20s %-20s %s", "Command", "Keys", "Help")
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(headers))
	lines = append(lines, strings.Repeat("─", 60))

	// Bindings list
	maxVisible := 20
	startIdx := 0
	if k.selectedIndex >= maxVisible {
		startIdx = k.selectedIndex - maxVisible + 1
	}

	for i := startIdx; i < len(k.config.Bindings) && i < startIdx+maxVisible; i++ {
		binding := k.config.Bindings[i]
		keys := strings.Join(binding.Keys, ", ")
		line := fmt.Sprintf("%-20s %-20s %s", binding.Command, keys, binding.Help)

		if i == k.selectedIndex {
			lines = append(lines, k.selectedStyle.Render(line))
		} else {
			lines = append(lines, k.itemStyle.Render(line))
		}
	}

	// Help text
	lines = append(lines, "")
	lines = append(lines, k.helpStyle.Render("↑/k:up  ↓/j:down  enter/e:edit  s:save  r:reset  q:quit"))

	// Check for conflicts
	conflicts := k.config.ValidateBindings()
	if len(conflicts) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("⚠ Conflicts detected:"))
		for key, commands := range conflicts {
			lines = append(lines, fmt.Sprintf("  %s → %s", key, strings.Join(commands, ", ")))
		}
	}

	return strings.Join(lines, "\n")
}

func (k *KeybindingEditorOverlay) renderEdit() string {
	var lines []string

	// Title
	lines = append(lines, k.titleStyle.Render("Edit Keybinding"))
	lines = append(lines, "")

	// Current binding info
	lines = append(lines, fmt.Sprintf("Command: %s", k.editingBinding.Command))
	lines = append(lines, fmt.Sprintf("Current keys: %s", strings.Join(k.editingBinding.Keys, ", ")))
	lines = append(lines, "")

	// New keys
	if k.captureNextKey {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Render("Press the key combination you want to assign..."))
		lines = append(lines, "(Press ESC to cancel)")
	} else {
		lines = append(lines, fmt.Sprintf("New keys: %s", strings.Join(k.editingKeys, ", ")))
		lines = append(lines, "")
		lines = append(lines, k.helpStyle.Render("enter:save  a:add key  d:delete last  esc:cancel"))
	}

	return strings.Join(lines, "\n")
}

func (k *KeybindingEditorOverlay) renderConfirm() string {
	var lines []string

	lines = append(lines, k.titleStyle.Render("Save Changes?"))
	lines = append(lines, "")
	lines = append(lines, "Save keyboard configuration changes?")
	lines = append(lines, "")
	lines = append(lines, k.helpStyle.Render("y:yes  n:no"))

	return strings.Join(lines, "\n")
}

// GetDimensions returns the dimensions of the overlay
func (k *KeybindingEditorOverlay) GetDimensions() (int, int) {
	return k.width, k.height
}
