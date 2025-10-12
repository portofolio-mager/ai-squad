package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// JavaScript syntax patterns
var (
	// Keywords
	jsKeywordRegex = regexp.MustCompile(`\b(async|await|break|case|catch|class|const|continue|debugger|default|delete|do|else|export|extends|finally|for|function|if|import|in|instanceof|let|new|of|return|super|switch|this|throw|try|typeof|var|void|while|with|yield)\b`)

	// Literals
	jsStringRegex  = regexp.MustCompile(`("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|` + "`" + `(?:[^` + "`" + `\\]|\\.)*` + "`" + `)`)
	jsNumberRegex  = regexp.MustCompile(`\b(\d+\.?\d*([eE][+-]?\d+)?|0x[0-9a-fA-F]+|0b[01]+|0o[0-7]+)\b`)
	jsBooleanRegex = regexp.MustCompile(`\b(true|false|null|undefined|NaN|Infinity)\b`)

	// Comments
	jsSingleCommentRegex = regexp.MustCompile(`//.*$`)
	jsMultiCommentRegex  = regexp.MustCompile(`/\*[\s\S]*?\*/`)

	// Functions and methods
	jsFunctionRegex = regexp.MustCompile(`\b([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(`)

	// Common objects and methods
	jsBuiltinRegex = regexp.MustCompile(`\b(console|document|window|Array|Object|String|Number|Boolean|Date|Math|JSON|Promise|Map|Set|Symbol|Error|RegExp)\b`)

	// Operators
	jsOperatorRegex = regexp.MustCompile(`(===|!==|==|!=|<=|>=|<|>|\+=|-=|\*=|\/=|%=|&&|\|\||!|\+\+|--|=>)`)
)

// Go syntax patterns
var (
	// Keywords
	goKeywordRegex = regexp.MustCompile(`\b(break|case|chan|const|continue|default|defer|else|fallthrough|for|func|go|goto|if|import|interface|map|package|range|return|select|struct|switch|type|var)\b`)

	// Built-in types and functions
	goBuiltinRegex = regexp.MustCompile(`\b(bool|byte|complex64|complex128|error|float32|float64|int|int8|int16|int32|int64|rune|string|uint|uint8|uint16|uint32|uint64|uintptr|true|false|nil|append|cap|close|complex|copy|delete|imag|len|make|new|panic|print|println|real|recover)\b`)

	// Common packages
	goPackageRegex = regexp.MustCompile(`\b(fmt|os|io|strings|strconv|time|errors|log|net|http|json|regexp|sort|sync|bytes|bufio|path|filepath|math|rand|reflect|runtime|testing)\b`)

	// Operators (including :=)
	goOperatorRegex = regexp.MustCompile(`(:=|==|!=|<=|>=|<|>|\+=|-=|\*=|\/=|%=|&=|\|=|\^=|<<=|>>=|&&|\|\||!|\+\+|--)`)
)

// Syntax highlighting styles
var (
	keywordStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("204")) // Pink
	stringStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))  // Green
	numberStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("209")) // Orange
	commentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243")) // Gray
	functionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))  // Blue
	builtinStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	operatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // Red
	defaultStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("252")) // Light gray
)

// HighlightJavaScript applies syntax highlighting to JavaScript code
func HighlightJavaScript(code string) string {
	lines := strings.Split(code, "\n")
	highlightedLines := make([]string, len(lines))

	for i, line := range lines {
		highlightedLines[i] = highlightJSLine(line)
	}

	return strings.Join(highlightedLines, "\n")
}

// highlightJSLine highlights a single line of JavaScript code
func highlightJSLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return line
	}

	// Create a structure to track replacements
	type replacement struct {
		start, end int
		text       string
	}

	var replacements []replacement

	// Helper to add a replacement
	addReplacement := func(start, end int, text string) {
		// Check for overlaps
		for _, r := range replacements {
			if (start >= r.start && start < r.end) || (end > r.start && end <= r.end) {
				return // Skip overlapping replacements
			}
		}
		replacements = append(replacements, replacement{start, end, text})
	}

	// Apply highlighting patterns
	// 1. Comments (highest priority)
	for _, match := range jsSingleCommentRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], commentStyle.Render(line[match[0]:match[1]]))
	}
	for _, match := range jsMultiCommentRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], commentStyle.Render(line[match[0]:match[1]]))
	}

	// 2. Strings
	for _, match := range jsStringRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], stringStyle.Render(line[match[0]:match[1]]))
	}

	// 3. Numbers
	for _, match := range jsNumberRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], numberStyle.Render(line[match[0]:match[1]]))
	}

	// 4. Booleans and special values
	for _, match := range jsBooleanRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], numberStyle.Render(line[match[0]:match[1]]))
	}

	// 5. Keywords
	for _, match := range jsKeywordRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], keywordStyle.Render(line[match[0]:match[1]]))
	}

	// 6. Built-in objects
	for _, match := range jsBuiltinRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], builtinStyle.Render(line[match[0]:match[1]]))
	}

	// 7. Functions
	matches := jsFunctionRegex.FindAllStringSubmatchIndex(line, -1)
	for _, match := range matches {
		if len(match) >= 4 {
			// match[2] and match[3] are the start and end of the function name (group 1)
			addReplacement(match[2], match[3], functionStyle.Render(line[match[2]:match[3]]))
		}
	}

	// 8. Operators
	for _, match := range jsOperatorRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], operatorStyle.Render(line[match[0]:match[1]]))
	}

	// Sort replacements by start position
	for i := 0; i < len(replacements); i++ {
		for j := i + 1; j < len(replacements); j++ {
			if replacements[j].start < replacements[i].start {
				replacements[i], replacements[j] = replacements[j], replacements[i]
			}
		}
	}

	// Build the final result
	var result strings.Builder
	lastEnd := 0

	for _, r := range replacements {
		// Add any unhighlighted text before this replacement
		if r.start > lastEnd {
			result.WriteString(defaultStyle.Render(line[lastEnd:r.start]))
		}
		// Add the highlighted text
		result.WriteString(r.text)
		lastEnd = r.end
	}

	// Add any remaining unhighlighted text
	if lastEnd < len(line) {
		result.WriteString(defaultStyle.Render(line[lastEnd:]))
	}

	return result.String()
}

// HighlightCode applies syntax highlighting based on the language
func HighlightCode(code, language string) string {
	switch strings.ToLower(language) {
	case "javascript", "js", "jsx":
		return HighlightJavaScript(code)
	case "typescript", "ts", "tsx":
		// TypeScript is similar to JavaScript, so use the same highlighter
		return HighlightJavaScript(code)
	case "go", "golang":
		return HighlightGo(code)
	case "suggestion":
		// GitHub suggestion blocks - try to detect the language
		if strings.Contains(code, ":=") || strings.Contains(code, "func ") || strings.Contains(code, "package ") {
			// Looks like Go code
			return HighlightGo(code)
		} else if strings.Contains(code, "const ") || strings.Contains(code, "let ") || strings.Contains(code, "var ") {
			// Looks like JavaScript
			return HighlightJavaScript(code)
		}
		// Default to basic highlighting
		return highlightBasic(code)
	default:
		// No highlighting for other languages yet
		return code
	}
}

// highlightBasic provides basic syntax highlighting for comments and strings
func highlightBasic(code string) string {
	lines := strings.Split(code, "\n")
	highlightedLines := make([]string, len(lines))

	for i, line := range lines {
		// Highlight single-line comments
		if idx := strings.Index(line, "//"); idx != -1 {
			highlightedLines[i] = defaultStyle.Render(line[:idx]) + commentStyle.Render(line[idx:])
		} else {
			highlightedLines[i] = defaultStyle.Render(line)
		}
	}

	return strings.Join(highlightedLines, "\n")
}

// HighlightGo applies syntax highlighting to Go code
func HighlightGo(code string) string {
	lines := strings.Split(code, "\n")
	highlightedLines := make([]string, len(lines))

	for i, line := range lines {
		highlightedLines[i] = highlightGoLine(line)
	}

	return strings.Join(highlightedLines, "\n")
}

// highlightGoLine highlights a single line of Go code
func highlightGoLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return line
	}

	// Create a structure to track replacements
	type replacement struct {
		start, end int
		text       string
	}

	var replacements []replacement

	// Helper to add a replacement
	addReplacement := func(start, end int, text string) {
		// Check for overlaps
		for _, r := range replacements {
			if (start >= r.start && start < r.end) || (end > r.start && end <= r.end) {
				return // Skip overlapping replacements
			}
		}
		replacements = append(replacements, replacement{start, end, text})
	}

	// Apply highlighting patterns
	// 1. Comments (highest priority)
	for _, match := range jsSingleCommentRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], commentStyle.Render(line[match[0]:match[1]]))
	}

	// 2. Strings (reuse JS string regex)
	for _, match := range jsStringRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], stringStyle.Render(line[match[0]:match[1]]))
	}

	// 3. Numbers (reuse JS number regex)
	for _, match := range jsNumberRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], numberStyle.Render(line[match[0]:match[1]]))
	}

	// 4. Go keywords
	for _, match := range goKeywordRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], keywordStyle.Render(line[match[0]:match[1]]))
	}

	// 5. Go built-ins
	for _, match := range goBuiltinRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], builtinStyle.Render(line[match[0]:match[1]]))
	}

	// 6. Go packages
	for _, match := range goPackageRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], functionStyle.Render(line[match[0]:match[1]]))
	}

	// 7. Operators
	for _, match := range goOperatorRegex.FindAllStringIndex(line, -1) {
		addReplacement(match[0], match[1], operatorStyle.Render(line[match[0]:match[1]]))
	}

	// Sort replacements by start position
	for i := 0; i < len(replacements); i++ {
		for j := i + 1; j < len(replacements); j++ {
			if replacements[j].start < replacements[i].start {
				replacements[i], replacements[j] = replacements[j], replacements[i]
			}
		}
	}

	// Build the final result
	var result strings.Builder
	lastEnd := 0

	for _, r := range replacements {
		// Add any unhighlighted text before this replacement
		if r.start > lastEnd {
			result.WriteString(defaultStyle.Render(line[lastEnd:r.start]))
		}
		// Add the highlighted text
		result.WriteString(r.text)
		lastEnd = r.end
	}

	// Add any remaining unhighlighted text
	if lastEnd < len(line) {
		result.WriteString(defaultStyle.Render(line[lastEnd:]))
	}

	return result.String()
}
