package ui

import (
	"fmt"
	"strings"

	"ai-squad/session/git"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type CommentSplitModel struct {
	comment      *git.PRComment
	currentIndex int
	width        int
	height       int
	showHelp     bool
	viewport     viewport.Model
	ready        bool
	editMode     bool
	textarea     textarea.Model
	editingIndex int
}

type CommentSplitCompleteMsg struct {
	Comment *git.PRComment
}

type CommentSplitCancelMsg struct{}

func NewCommentSplitModel(comment *git.PRComment) CommentSplitModel {
	// Split the comment into pieces if not already split
	if !comment.IsSplit {
		comment.SplitIntoPieces()
	}

	return CommentSplitModel{
		comment:      comment,
		currentIndex: 0,
		showHelp:     true,
		ready:        false,
		width:        80,
		height:       24,
		editMode:     false,
		editingIndex: -1,
	}
}

func (m CommentSplitModel) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m CommentSplitModel) Update(msg tea.Msg) (CommentSplitModel, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	// Handle edit mode first
	if m.editMode {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				// Cancel edit mode without saving
				m.editMode = false
				m.editingIndex = -1
				if m.ready {
					m.updateViewportContent()
				}
				return m, nil

			case "ctrl+s", "enter":
				// Save the edited content
				if m.editingIndex >= 0 && m.editingIndex < len(m.comment.SplitPieces) {
					m.comment.SplitPieces[m.editingIndex].Content = m.textarea.Value()
				}
				m.editMode = false
				m.editingIndex = -1
				if m.ready {
					m.updateViewportContent()
				}
				return m, nil

			default:
				// Pass other keys to textarea
				m.textarea, cmd = m.textarea.Update(msg)
				return m, cmd
			}
		default:
			// Pass other messages to textarea
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 4 // Title + status + blank line
		footerHeight := 2 // Help text
		if !m.showHelp {
			footerHeight = 0
		}

		if !m.ready {
			m.viewport = viewport.New(m.width, m.height-headerHeight-footerHeight)
			m.viewport.HighPerformanceRendering = false
			m.ready = true
			m.viewport.SetYOffset(0)
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = m.height - headerHeight - footerHeight
		}

		m.updateViewportContent()
		m.viewport.SetYOffset(0)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			return m, func() tea.Msg { return CommentSplitCancelMsg{} }

		case "?":
			m.showHelp = !m.showHelp
			if m.ready {
				headerHeight := 4
				footerHeight := 2
				if !m.showHelp {
					footerHeight = 0
				}
				m.viewport.Height = m.height - headerHeight - footerHeight
				m.updateViewportContent()
			}
			return m, nil

		case "j", "down":
			if m.currentIndex < len(m.comment.SplitPieces)-1 {
				m.currentIndex++
				if m.ready {
					m.updateViewportContent()
					m.ensureCurrentPieceVisible()
				}
			}
			return m, nil

		case "k", "up":
			if m.currentIndex > 0 {
				m.currentIndex--
				if m.ready {
					m.updateViewportContent()
					m.ensureCurrentPieceVisible()
				}
			}
			return m, nil

		case "a":
			if len(m.comment.SplitPieces) > 0 {
				m.comment.SplitPieces[m.currentIndex].Accepted = true
				if m.ready {
					m.updateViewportContent()
				}
			}
			return m, nil

		case "d":
			if len(m.comment.SplitPieces) > 0 {
				m.comment.SplitPieces[m.currentIndex].Accepted = false
				if m.ready {
					m.updateViewportContent()
				}
			}
			return m, nil

		case "A":
			for i := range m.comment.SplitPieces {
				m.comment.SplitPieces[i].Accepted = true
			}
			if m.ready {
				m.updateViewportContent()
			}
			return m, nil

		case "D":
			for i := range m.comment.SplitPieces {
				m.comment.SplitPieces[i].Accepted = false
			}
			if m.ready {
				m.updateViewportContent()
			}
			return m, nil

		case "e":
			// Enter edit mode for current piece
			if len(m.comment.SplitPieces) > 0 {
				m.editMode = true
				m.editingIndex = m.currentIndex

				// Initialize textarea with current content
				ta := textarea.New()
				ta.SetValue(m.comment.SplitPieces[m.currentIndex].Content)
				ta.SetWidth(m.width - 4)
				ta.SetHeight(10)
				ta.Focus()
				m.textarea = ta
			}
			return m, textarea.Blink

		case "m":
			// Merge current piece with next
			if m.currentIndex < len(m.comment.SplitPieces)-1 {
				// Get pointer to modify in place
				current := &m.comment.SplitPieces[m.currentIndex]
				next := m.comment.SplitPieces[m.currentIndex+1]

				// Merge content
				current.Content = current.Content + "\n\n" + next.Content
				current.Accepted = current.Accepted || next.Accepted // Keep accepted if either was accepted

				// Remove the next piece
				m.comment.SplitPieces = append(
					m.comment.SplitPieces[:m.currentIndex+1],
					m.comment.SplitPieces[m.currentIndex+2:]...,
				)

				if m.ready {
					m.updateViewportContent()
				}
			}
			return m, nil

		case "enter":
			return m, func() tea.Msg { return CommentSplitCompleteMsg{Comment: m.comment} }

		case "pgup", "shift+up":
			if m.ready {
				m.viewport.ViewUp()
			}
			return m, nil

		case "pgdown", "shift+down":
			if m.ready {
				m.viewport.ViewDown()
			}
			return m, nil

		case "home", "g":
			if m.ready {
				m.viewport.GotoTop()
			}
			if len(m.comment.SplitPieces) > 0 {
				m.currentIndex = 0
				if m.ready {
					m.updateViewportContent()
				}
			}
			return m, nil

		case "end", "G":
			if m.ready {
				m.viewport.GotoBottom()
			}
			if len(m.comment.SplitPieces) > 0 {
				m.currentIndex = len(m.comment.SplitPieces) - 1
				if m.ready {
					m.updateViewportContent()
				}
			}
			return m, nil
		}
	}

	// Handle viewport updates
	if m.ready && !m.editMode {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m CommentSplitModel) View() string {
	if m.editMode {
		return m.editView()
	}

	if !m.ready {
		return m.simpleView()
	}

	// Build header
	var header strings.Builder
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	header.WriteString(headerStyle.Render("Comment Split Mode"))
	header.WriteString("\n")

	// Show comment info
	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
	header.WriteString(infoStyle.Render(fmt.Sprintf("@%s • %s", m.comment.Author, m.comment.Type)))
	header.WriteString("\n")

	// Show piece statistics
	acceptedCount := 0
	for _, piece := range m.comment.SplitPieces {
		if piece.Accepted {
			acceptedCount++
		}
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
	header.WriteString(statusStyle.Render(fmt.Sprintf("Pieces: %d/%d • Accepted: %d",
		m.currentIndex+1, len(m.comment.SplitPieces), acceptedCount)))
	header.WriteString("\n")

	// Build footer
	var footer string
	if m.showHelp {
		helpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

		helpItems := []string{
			"j/k:nav",
			"a/d:accept/deny",
			"e:edit",
			"m:merge",
			"Enter:done",
			"q:cancel",
			"?:help",
		}
		footer = "\n" + helpStyle.Render(strings.Join(helpItems, " • "))
	}

	return header.String() + m.viewport.View() + footer
}

func (m *CommentSplitModel) updateViewportContent() {
	var content strings.Builder

	for i, piece := range m.comment.SplitPieces {
		if i > 0 {
			content.WriteString("\n\n")
		}

		// Piece box styling
		var boxStyle lipgloss.Style
		isSelected := i == m.currentIndex

		maxWidth := m.width - 4
		if maxWidth < 40 {
			maxWidth = 40
		}

		if isSelected {
			boxStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("86")).
				Padding(0, 1).
				Width(maxWidth)
		} else {
			boxStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.HiddenBorder()).
				Padding(0, 1).
				Width(maxWidth)
		}

		// Status indicator
		status := "[ ]"
		if piece.Accepted {
			status = "[✓]"
		}

		// Build header
		header := fmt.Sprintf("%s Piece %d/%d", status, i+1, len(m.comment.SplitPieces))

		// Show content preview
		contentPreview := piece.Content
		if !isSelected && len(contentPreview) > 100 {
			contentPreview = contentPreview[:97] + "..."
		}

		// Wrap text
		lines := m.wrapText(contentPreview, maxWidth-4)
		wrappedContent := strings.Join(lines, "\n")

		// Combine header and content
		pieceContent := fmt.Sprintf("%s\n\n%s",
			lipgloss.NewStyle().Bold(true).Render(header),
			wrappedContent)

		// Add selection indicator
		prefix := "  "
		if isSelected {
			prefix = "> "
		}

		content.WriteString(prefix + boxStyle.Render(pieceContent))
	}

	// Add padding
	content.WriteString("\n\n\n\n")

	m.viewport.SetContent(content.String())
}

func (m *CommentSplitModel) wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	var result []string
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		if len(line) <= width {
			result = append(result, line)
			continue
		}

		// Wrap long lines
		for len(line) > width {
			// Find last space before width
			lastSpace := width
			for i := width; i > 0; i-- {
				if line[i-1] == ' ' {
					lastSpace = i
					break
				}
			}

			// If no space found, just cut at width
			if lastSpace == width {
				result = append(result, line[:width])
				line = line[width:]
			} else {
				result = append(result, line[:lastSpace-1])
				line = line[lastSpace:]
			}
		}

		if len(line) > 0 {
			result = append(result, line)
		}
	}

	return result
}

func (m *CommentSplitModel) ensureCurrentPieceVisible() {
	// Simple implementation - could be improved
	lines := strings.Split(m.viewport.View(), "\n")
	totalLines := len(lines)

	if totalLines == 0 {
		return
	}

	// Estimate position based on piece index
	estimatedPosition := float64(m.currentIndex) / float64(len(m.comment.SplitPieces))
	targetLine := int(estimatedPosition * float64(m.viewport.TotalLineCount()))

	// Scroll to make the piece visible
	if targetLine < m.viewport.YOffset {
		m.viewport.SetYOffset(targetLine)
	} else if targetLine > m.viewport.YOffset+m.viewport.Height-5 {
		m.viewport.SetYOffset(targetLine - m.viewport.Height + 5)
	}
}

func (m CommentSplitModel) simpleView() string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	b.WriteString(headerStyle.Render("Comment Split Mode"))
	b.WriteString("\n\n")

	// Show current piece
	if len(m.comment.SplitPieces) > 0 && m.currentIndex < len(m.comment.SplitPieces) {
		piece := m.comment.SplitPieces[m.currentIndex]

		status := "[ ]"
		if piece.Accepted {
			status = "[✓]"
		}

		b.WriteString(fmt.Sprintf("Piece %d/%d:\n", m.currentIndex+1, len(m.comment.SplitPieces)))
		b.WriteString(fmt.Sprintf("%s\n\n", status))

		content := piece.Content
		if len(content) > 300 {
			content = content[:297] + "..."
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	b.WriteString(helpStyle.Render("Keys: j/k:nav • a/d:accept/deny • e:edit • Enter:done • q:cancel"))

	return b.String()
}

func (m CommentSplitModel) editView() string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	b.WriteString(headerStyle.Render("Edit Piece"))
	b.WriteString("\n\n")

	b.WriteString(m.textarea.View())
	b.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	b.WriteString(helpStyle.Render("Ctrl+S/Enter: Save • Esc: Cancel"))

	return b.String()
}
