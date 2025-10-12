package keys

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
)

type KeyName int

const (
	KeyUp KeyName = iota
	KeyDown
	KeyEnter
	KeyNew
	KeyKill
	KeyQuit
	KeyReview
	KeyPush
	KeySubmit
	KeyPRReview
	KeyPRResolveConversations

	KeyTab        // Tab is a special keybinding for switching between panes.
	KeyShiftTab   // ShiftTab is a special keybinding for switching between panes in reverse.
	KeySubmitName // SubmitName is a special keybinding for submitting the name of a new instance.

	KeyCheckout
	KeyResume
	KeyPrompt         // New key for entering a prompt
	KeyHelp           // Key for showing help screen
	KeyChangeProgram  // Key for changing the program
	KeyListProgram    // Key for listing the current program
	KeyExistingBranch // Key for creating instance from existing branch
	KeyErrorLog       // Key for showing error log
	KeyOpenIDE        // Key for opening IDE
	KeyRebase         // Key for rebasing with main branch
	KeyBookmark       // Key for creating a bookmark commit
	KeyTest           // Key for running Jest tests
	KeyExternalDiff   // Key for opening in external diff tool

	// Jest keybindings
	KeyJestNextFailure     // Key for navigating to next Jest failure
	KeyJestPreviousFailure // Key for navigating to previous Jest failure
	KeyJestOpenInIDE       // Key for opening Jest test failure in IDE
	KeyJestRerun           // Key for rerunning Jest tests

	// Diff keybindings
	KeyShiftUp
	KeyShiftDown
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyAltUp
	KeyAltDown
	KeyDiffAll
	KeyDiffLastCommit
	KeyLeft
	KeyRight
	KeyScrollLock
	KeyOpenInIDE
	KeyHistory
	KeyEditKeybindings   // Key for opening keybinding editor
	KeyGitStatus         // Key for showing git status overlay
	KeyGitStatusBookmark // Key for showing git status overlay in bookmark mode
	KeyCheckUpdate       // Key for checking for updates
	KeyGitReset          // Key for git reset --hard origin/branch
)

// GlobalKeyStringsMap is a global, immutable map string to keybinding.
var GlobalKeyStringsMap = map[string]KeyName{
	"up":         KeyUp,
	"k":          KeyUp,
	"down":       KeyDown,
	"j":          KeyDown,
	"shift+up":   KeyShiftUp,
	"shift+down": KeyShiftDown,
	"home":       KeyHome,
	"end":        KeyEnd,
	"ctrl+a":     KeyHome,
	"ctrl+e":     KeyEnd,
	"ctrl+home":  KeyHome,
	"ctrl+end":   KeyEnd,
	"pgup":       KeyPageUp,
	"pgdown":     KeyPageDown,
	"alt+up":     KeyAltUp,
	"alt+down":   KeyAltDown,
	"a":          KeyDiffAll,
	"d":          KeyDiffLastCommit,
	"left":       KeyLeft,
	"h":          KeyLeft,
	"right":      KeyRight,
	"l":          KeyRight,
	"s":          KeyScrollLock,
	"N":          KeyPrompt,
	"enter":      KeyEnter,
	"o":          KeyEnter,
	"n":          KeyNew,
	"e":          KeyExistingBranch,
	"D":          KeyKill,
	"q":          KeyQuit,
	"tab":        KeyTab,
	"shift+tab":  KeyShiftTab,
	"c":          KeyCheckout,
	"r":          KeyResume,
	"p":          KeySubmit,
	"P":          KeyChangeProgram,
	"A":          KeyChangeProgram,
	"L":          KeyListProgram,
	"?":          KeyHelp,
	"E":          KeyErrorLog,
	"w":          KeyOpenIDE,
	"i":          KeyOpenInIDE,
	"b":          KeyRebase,
	"B":          KeyBookmark,
	"R":          KeyPRReview,
	"ctrl+r":     KeyPRResolveConversations,
	"ctrl+h":     KeyHistory,
	"K":          KeyEditKeybindings,
	"t":          KeyTest,
	"x":          KeyExternalDiff,
	"g":          KeyGitStatus,
	"G":          KeyGitStatusBookmark,
	"U":          KeyCheckUpdate,
	"S":          KeyGitReset,

	// Jest navigation - these are only active in Jest tab
	// "n" and "p" are already taken globally, so we'll handle them contextually
	// "enter" and "r" will also be handled contextually in Jest tab
}

// GlobalkeyBindings is a global, immutable map of KeyName tot keybinding.
var GlobalkeyBindings = map[KeyName]key.Binding{
	KeyUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	KeyDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	KeyShiftUp: key.NewBinding(
		key.WithKeys("shift+up"),
		key.WithHelp("shift+↑", "scroll"),
	),
	KeyShiftDown: key.NewBinding(
		key.WithKeys("shift+down"),
		key.WithHelp("shift+↓", "scroll"),
	),
	KeyHome: key.NewBinding(
		key.WithKeys("home", "ctrl+a", "ctrl+home"),
		key.WithHelp("home/ctrl+a", "scroll to top"),
	),
	KeyEnd: key.NewBinding(
		key.WithKeys("end", "ctrl+e", "ctrl+end"),
		key.WithHelp("end/ctrl+e", "scroll to bottom"),
	),
	KeyPageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up"),
	),
	KeyPageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdn", "page down"),
	),
	KeyAltUp: key.NewBinding(
		key.WithKeys("alt+up"),
		key.WithHelp("alt+↑", "prev file"),
	),
	KeyAltDown: key.NewBinding(
		key.WithKeys("alt+down"),
		key.WithHelp("alt+↓", "next file"),
	),
	KeyDiffAll: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "all changes"),
	),
	KeyDiffLastCommit: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "last commit diff"),
	),
	KeyLeft: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "prev commit"),
	),
	KeyRight: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "next commit"),
	),
	KeyScrollLock: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "toggle scroll lock"),
	),
	KeyOpenInIDE: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "open in IDE"),
	),
	KeyEnter: key.NewBinding(
		key.WithKeys("enter", "o"),
		key.WithHelp("↵/o", "open"),
	),
	KeyNew: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new"),
	),
	KeyExistingBranch: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "existing branch"),
	),
	KeyKill: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "kill"),
	),
	KeyHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	KeyQuit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	KeySubmit: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "push branch"),
	),
	KeyPrompt: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "new with prompt"),
	),
	KeyCheckout: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "checkout"),
	),
	KeyTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch tab"),
	),
	KeyShiftTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "switch tab (reverse)"),
	),
	KeyResume: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "resume"),
	),
	KeyChangeProgram: key.NewBinding(
		key.WithKeys("P", "a"),
		key.WithHelp("P/a", "change program"),
	),
	KeyListProgram: key.NewBinding(
		key.WithKeys("L"),
		key.WithHelp("L", "current program"),
	),
	KeyErrorLog: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "error log"),
	),
	KeyOpenIDE: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "open IDE"),
	),
	KeyRebase: key.NewBinding(
		key.WithKeys("b"),
		key.WithHelp("b", "rebase"),
	),
	KeyPRReview: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "review PR comments"),
	),
	KeyPRResolveConversations: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "resolve all PR conversations"),
	),
	KeyBookmark: key.NewBinding(
		key.WithKeys("B"),
		key.WithHelp("B", "bookmark"),
	),
	KeyHistory: key.NewBinding(
		key.WithKeys("ctrl+h"),
		key.WithHelp("ctrl+h", "view history"),
	),
	KeyEditKeybindings: key.NewBinding(
		key.WithKeys("K"),
		key.WithHelp("K", "edit keys"),
	),
	KeyTest: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "run tests"),
	),
	KeyExternalDiff: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "external diff"),
	),
	KeyGitStatus: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "git status"),
	),
	KeyGitStatusBookmark: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "git status bookmarks"),
	),
	KeyCheckUpdate: key.NewBinding(
		key.WithKeys("U"),
		key.WithHelp("U", "check for updates"),
	),
	KeyGitReset: key.NewBinding(
		key.WithKeys("h"),
		key.WithHelp("h", "git reset --hard"),
	),

	// -- Special keybindings --

	KeySubmitName: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit name"),
	),
}

// CustomKeyStringsMap is a mutable map that can be updated with custom keybindings
var CustomKeyStringsMap map[string]KeyName

// KeyBinding represents a custom keybinding configuration
type KeyBinding struct {
	Command string   `json:"command"` // The command name (e.g., "up", "down", "new")
	Keys    []string `json:"keys"`    // The key combinations (e.g., ["k", "up"])
	Help    string   `json:"help"`    // Help text to display
}

// KeyBindingsConfig stores all custom keybindings
type KeyBindingsConfig struct {
	Version  string       `json:"version"`  // Config version for future migrations
	Bindings []KeyBinding `json:"bindings"` // List of custom keybindings
}

// InitializeCustomKeyBindings loads custom keybindings from config
func InitializeCustomKeyBindings() error {
	// Load keybindings config
	kbConfig, err := LoadKeyBindings()
	if err != nil {
		return err
	}

	// Convert to key map
	CustomKeyStringsMap = kbConfig.ToKeyMap()

	// Also update the GlobalkeyBindings with custom keys
	updateGlobalBindings(kbConfig)

	return nil
}

// GetKeyName returns the KeyName for a given key string, checking custom bindings first
func GetKeyName(keyStr string) (KeyName, bool) {
	// Check custom bindings first
	if CustomKeyStringsMap != nil {
		if keyName, ok := CustomKeyStringsMap[keyStr]; ok {
			return keyName, true
		}
	}

	// Fall back to default bindings
	keyName, ok := GlobalKeyStringsMap[keyStr]
	return keyName, ok
}

// DefaultKeyBindings returns the default keybindings configuration
func DefaultKeyBindings() *KeyBindingsConfig {
	return &KeyBindingsConfig{
		Version: "1.0",
		Bindings: []KeyBinding{
			// Navigation
			{Command: "up", Keys: []string{"up", "k"}, Help: "↑/k"},
			{Command: "down", Keys: []string{"down", "j"}, Help: "↓/j"},
			{Command: "home", Keys: []string{"home", "ctrl+a", "ctrl+home"}, Help: "home/ctrl+a"},
			{Command: "end", Keys: []string{"end", "ctrl+e", "ctrl+end"}, Help: "end/ctrl+e"},
			{Command: "page_up", Keys: []string{"pgup"}, Help: "pgup"},
			{Command: "page_down", Keys: []string{"pgdown"}, Help: "pgdn"},

			// Instance management
			{Command: "new", Keys: []string{"n"}, Help: "n"},
			{Command: "new_with_prompt", Keys: []string{"N"}, Help: "N"},
			{Command: "existing_branch", Keys: []string{"e"}, Help: "e"},
			{Command: "kill", Keys: []string{"D"}, Help: "D"},
			{Command: "checkout", Keys: []string{"c"}, Help: "c"},
			{Command: "resume", Keys: []string{"r"}, Help: "r"},
			{Command: "push", Keys: []string{"p"}, Help: "p"},
			{Command: "rebase", Keys: []string{"b"}, Help: "b"},

			// Diff view
			{Command: "scroll_up", Keys: []string{"shift+up"}, Help: "shift+↑"},
			{Command: "scroll_down", Keys: []string{"shift+down"}, Help: "shift+↓"},
			{Command: "prev_file", Keys: []string{"alt+up"}, Help: "alt+↑"},
			{Command: "next_file", Keys: []string{"alt+down"}, Help: "alt+↓"},
			{Command: "diff_all", Keys: []string{"a"}, Help: "a"},
			{Command: "diff_last_commit", Keys: []string{"d"}, Help: "d"},
			{Command: "prev_commit", Keys: []string{"left"}, Help: "←"},
			{Command: "next_commit", Keys: []string{"right"}, Help: "→"},
			{Command: "scroll_lock", Keys: []string{"s"}, Help: "s"},

			// Actions
			{Command: "enter", Keys: []string{"enter", "o"}, Help: "↵/o"},
			{Command: "tab", Keys: []string{"tab"}, Help: "tab"},
			{Command: "shift_tab", Keys: []string{"shift+tab"}, Help: "shift+tab"},
			{Command: "help", Keys: []string{"?"}, Help: "?"},
			{Command: "quit", Keys: []string{"q"}, Help: "q"},
			{Command: "error_log", Keys: []string{"l"}, Help: "l"},
			{Command: "webstorm", Keys: []string{"w"}, Help: "w"},
			{Command: "open_in_ide", Keys: []string{"i"}, Help: "i"},
			{Command: "edit_keybindings", Keys: []string{"K"}, Help: "K"},
			{Command: "git_status", Keys: []string{"g"}, Help: "g"},
			{Command: "git_status_bookmark", Keys: []string{"G"}, Help: "G"},
			{Command: "check_update", Keys: []string{"U"}, Help: "U"},
			{Command: "git_reset", Keys: []string{"h"}, Help: "h"},
		},
	}
}

// LoadKeyBindings loads keybindings from the config file
func LoadKeyBindings() (*KeyBindingsConfig, error) {
	configPath := filepath.Join(os.Getenv("HOME"), ".ai-squad", "keybindings.json")

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Return defaults if file doesn't exist
		return DefaultKeyBindings(), nil
	}

	// Read the file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// Parse JSON
	var config KeyBindingsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// If no bindings are defined, use defaults
	if len(config.Bindings) == 0 {
		return DefaultKeyBindings(), nil
	}

	return &config, nil
}

// Save saves keybindings to the config file
func (k *KeyBindingsConfig) Save() error {
	configPath := filepath.Join(os.Getenv("HOME"), ".ai-squad", "keybindings.json")

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Marshal to JSON with pretty printing
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(configPath, data, 0644)
}

// ToKeyMap converts the keybindings config to a map for easy lookup
func (k *KeyBindingsConfig) ToKeyMap() map[string]KeyName {
	keyMap := make(map[string]KeyName)

	// Map command names to KeyName constants
	commandToKeyName := getCommandToKeyNameMap()

	// Build the key map
	for _, binding := range k.Bindings {
		if keyName, ok := commandToKeyName[binding.Command]; ok {
			for _, key := range binding.Keys {
				keyMap[key] = keyName
			}
		}
	}

	return keyMap
}

// GetBinding returns the keybinding for a specific command
func (k *KeyBindingsConfig) GetBinding(command string) *KeyBinding {
	for _, binding := range k.Bindings {
		if binding.Command == command {
			return &binding
		}
	}
	return nil
}

// SetBinding updates or adds a keybinding for a command
func (k *KeyBindingsConfig) SetBinding(command string, keys []string, help string) {
	for i, binding := range k.Bindings {
		if binding.Command == command {
			k.Bindings[i].Keys = keys
			k.Bindings[i].Help = help
			return
		}
	}

	// Add new binding if not found
	k.Bindings = append(k.Bindings, KeyBinding{
		Command: command,
		Keys:    keys,
		Help:    help,
	})
}

// ValidateBindings checks for conflicts in keybindings
func (k *KeyBindingsConfig) ValidateBindings() map[string][]string {
	conflicts := make(map[string][]string)
	keyToCommands := make(map[string][]string)

	// Build map of keys to commands
	for _, binding := range k.Bindings {
		for _, key := range binding.Keys {
			keyToCommands[key] = append(keyToCommands[key], binding.Command)
		}
	}

	// Find conflicts
	for key, commands := range keyToCommands {
		if len(commands) > 1 {
			conflicts[key] = commands
		}
	}

	return conflicts
}

// getCommandToKeyNameMap returns the mapping of command names to KeyName constants
func getCommandToKeyNameMap() map[string]KeyName {
	return map[string]KeyName{
		"up":                  KeyUp,
		"down":                KeyDown,
		"enter":               KeyEnter,
		"new":                 KeyNew,
		"new_with_prompt":     KeyPrompt,
		"existing_branch":     KeyExistingBranch,
		"kill":                KeyKill,
		"quit":                KeyQuit,
		"push":                KeySubmit,
		"checkout":            KeyCheckout,
		"resume":              KeyResume,
		"help":                KeyHelp,
		"error_log":           KeyErrorLog,
		"open_ide":            KeyOpenIDE,
		"rebase":              KeyRebase,
		"tab":                 KeyTab,
		"shift_tab":           KeyShiftTab,
		"scroll_up":           KeyShiftUp,
		"scroll_down":         KeyShiftDown,
		"home":                KeyHome,
		"end":                 KeyEnd,
		"page_up":             KeyPageUp,
		"page_down":           KeyPageDown,
		"prev_file":           KeyAltUp,
		"next_file":           KeyAltDown,
		"diff_all":            KeyDiffAll,
		"diff_last_commit":    KeyDiffLastCommit,
		"prev_commit":         KeyLeft,
		"next_commit":         KeyRight,
		"scroll_lock":         KeyScrollLock,
		"open_in_ide":         KeyOpenInIDE,
		"edit_keybindings":    KeyEditKeybindings,
		"external_diff":       KeyExternalDiff,
		"git_status":          KeyGitStatus,
		"git_status_bookmark": KeyGitStatusBookmark,
		"check_update":        KeyCheckUpdate,
		"git_reset":           KeyGitReset,
	}
}

// updateGlobalBindings updates the GlobalkeyBindings with custom keybindings
func updateGlobalBindings(kbConfig *KeyBindingsConfig) {
	// Map command names to KeyName constants
	commandToKeyName := getCommandToKeyNameMap()

	// Update each binding
	for _, binding := range kbConfig.Bindings {
		if keyName, ok := commandToKeyName[binding.Command]; ok {
			// Update the global binding
			GlobalkeyBindings[keyName] = key.NewBinding(
				key.WithKeys(binding.Keys...),
				key.WithHelp(binding.Help, getHelpText(binding.Command)),
			)
		}
	}
}

// getHelpText returns the help text for a command
func getHelpText(command string) string {
	helpTexts := map[string]string{
		"up":                  "up",
		"down":                "down",
		"enter":               "open",
		"new":                 "new",
		"new_with_prompt":     "new with prompt",
		"existing_branch":     "existing branch",
		"kill":                "kill",
		"quit":                "quit",
		"push":                "push branch",
		"checkout":            "checkout",
		"resume":              "resume",
		"help":                "help",
		"error_log":           "error log",
		"open_ide":            "open IDE",
		"rebase":              "rebase",
		"tab":                 "switch tab",
		"shift_tab":           "switch tab (reverse)",
		"scroll_up":           "scroll",
		"scroll_down":         "scroll",
		"home":                "scroll to top",
		"end":                 "scroll to bottom",
		"page_up":             "page up",
		"page_down":           "page down",
		"prev_file":           "prev file",
		"next_file":           "next file",
		"diff_all":            "all changes",
		"diff_last_commit":    "last commit diff",
		"prev_commit":         "prev commit",
		"next_commit":         "next commit",
		"scroll_lock":         "toggle scroll lock",
		"open_in_ide":         "open in IDE",
		"external_diff":       "external diff",
		"git_status":          "git status",
		"git_status_bookmark": "git status bookmarks",
		"check_update":        "check for updates",
		"git_reset":           "git reset --hard",
	}

	if text, ok := helpTexts[command]; ok {
		return text
	}
	return command
}
