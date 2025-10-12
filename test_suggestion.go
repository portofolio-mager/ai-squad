//go:build ignore
// +build ignore

package main

import (
	"claude-squad/ui"
	"fmt"
)

func main() {
	testContent := `The magic number 11 for viewportHeight calculation should be defined as a constant or calculated from the individual component heights mentioned in the comment to improve maintainability.
` + "```suggestion" + `
    // Define constants for component heights
    const borderHeight = 2 // Top and bottom border
    const paddingHeight = 2 // Padding
    const headerHeight = 5 // Header (maximum height)
    const helpHeight = 2 // Help section
    
    // Calculate total height of non-viewport components
    totalNonViewportHeight := borderHeight + paddingHeight + headerHeight + helpHeight
    
    // Calculate viewport dimensions
    viewportHeight := height - totalNonViewportHeight
` + "```"

	fmt.Println("=== Original ===")
	fmt.Println(testContent)
	fmt.Println("\n=== Rendered ===")
	rendered := ui.RenderMarkdownLight(testContent)
	fmt.Println(rendered)
}
