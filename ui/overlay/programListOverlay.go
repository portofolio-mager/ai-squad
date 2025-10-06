package overlay

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProgramListOverlay displays a simple selectable list of programs.
// Navigation: Up / Down arrows
// Select: Enter
// Cancel: Esc / Ctrl+C
type ProgramListOverlay struct {
	programs  []string
	selected  int
	Submitted bool
	Canceled  bool
	width     int
	height    int
	Title     string
}

// NewProgramListOverlay creates a program list overlay and pre-selects the current value if present.
func NewProgramListOverlay(programs []string, current string) *ProgramListOverlay {
	sel := 0
	for i, p := range programs {
		if p == current {
			sel = i
			break
		}
	}
	return &ProgramListOverlay{
		programs:  programs,
		selected:  sel,
		Submitted: false,
		Canceled:  false,
		Title:     "Change program",
	}
}

// SetSize sets the overlay width/height used for rendering.
func (p *ProgramListOverlay) SetSize(width, height int) {
	p.width = width
	p.height = height
}

// Init is a no-op to satisfy common overlay semantics.
func (p *ProgramListOverlay) Init() tea.Cmd { return nil }

// IsSubmitted returns whether the user selected a program.
func (p *ProgramListOverlay) IsSubmitted() bool { return p.Submitted }

// GetSelected returns the currently selected program string.
func (p *ProgramListOverlay) GetSelected() string {
	if len(p.programs) == 0 {
		return ""
	}
	if p.selected < 0 || p.selected >= len(p.programs) {
		return ""
	}
	return p.programs[p.selected]
}

// HandleKeyPress handles arrow navigation and selection keys.
// Returns true when the overlay should be closed (Enter or Esc).
func (p *ProgramListOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyUp:
		if p.selected > 0 {
			p.selected--
		}
		return false
	case tea.KeyDown:
		if p.selected < len(p.programs)-1 {
			p.selected++
		}
		return false
	case tea.KeyEnter:
		p.Submitted = true
		return true
	case tea.KeyEsc:
		p.Canceled = true
		return true
	case tea.KeyRunes:
		// Allow selecting by number key (1..9)
		if len(msg.Runes) == 1 {
			r := msg.Runes[0]
			if r == 'q' {
				p.Canceled = true
				return true
			}

			if r >= '1' && r <= '9' {
				idx := int(r - '1')
				if idx >= 0 && idx < len(p.programs) {
					p.selected = idx
					p.Submitted = true
					return true
				}
			}
		}
	}
	return false
}

// Render returns the visual representation of the overlay.
func (p *ProgramListOverlay) Render() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(p.width)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("62")).
		Bold(true).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#dde4f0")).
		Foreground(lipgloss.Color("#1a1a1a")).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Padding(0, 1)

	var b strings.Builder
	b.WriteString(titleStyle.Render(p.Title) + "\n")

	for i, prog := range p.programs {
		prefix := fmt.Sprintf("%d.", i+1)
		var line string
		if i == p.selected {
			line = selectedStyle.Render(fmt.Sprintf("%s %s", prefix, prog))
		} else {
			line = normalStyle.Render(fmt.Sprintf("%s %s", prefix, prog))
		}
		b.WriteString(line)
		if i != len(p.programs)-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n\nUse ↑/↓ to navigate, Enter to select, Esc to cancel")

	return style.Render(b.String())
}
