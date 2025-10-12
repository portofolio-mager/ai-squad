package ui

import (
	"strings"
	"testing"
)

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Bold text",
			input:    "This is **bold** text",
			expected: "This is bold text",
		},
		{
			name:     "Italic text",
			input:    "This is *italic* text",
			expected: "This is italic text",
		},
		{
			name:     "Code inline",
			input:    "Use `git commit` to save",
			expected: "Use git commit to save",
		},
		{
			name:     "Headers",
			input:    "# Header 1\n## Header 2\nNormal text",
			expected: "Header 1\nHeader 2\nNormal text",
		},
		{
			name:     "Links",
			input:    "Check [this link](https://example.com) for more",
			expected: "Check this link for more",
		},
		{
			name:     "Mixed formatting",
			input:    "# Title\n**Bold** and *italic* with `code`",
			expected: "Title\nBold and italic with code",
		},
		{
			name:     "Code blocks",
			input:    "```\ncode block\n```\nNormal text",
			expected: "\ncode block\n\nNormal text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("StripMarkdown() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRenderMarkdown(t *testing.T) {
	// Basic test to ensure RenderMarkdown doesn't panic
	tests := []struct {
		name  string
		input string
		width int
	}{
		{
			name:  "Simple text",
			input: "Hello world",
			width: 80,
		},
		{
			name:  "Headers",
			input: "# Main Title\n## Subtitle\nContent here",
			width: 80,
		},
		{
			name:  "Code block",
			input: "```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```",
			width: 80,
		},
		{
			name:  "List",
			input: "- Item 1\n- Item 2\n  - Nested item",
			width: 80,
		},
		{
			name:  "Narrow width",
			input: "This is a long line that should wrap when rendered in a narrow terminal",
			width: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderMarkdown(tt.input, tt.width)
			if err != nil {
				t.Errorf("RenderMarkdown() error = %v", err)
				return
			}
			// Just check that we got some output
			if len(result) == 0 {
				t.Errorf("RenderMarkdown() returned empty string")
			}
			// Ensure no trailing newlines (our implementation strips them)
			if strings.HasSuffix(result, "\n") {
				t.Errorf("RenderMarkdown() has trailing newline")
			}
		})
	}
}
