package tui

import "github.com/charmbracelet/lipgloss"

// centerInTerminal places rendered content in the middle of the current
// terminal size without clipping when Bubble Tea has not reported dimensions or
// the terminal is smaller than the content.
func centerInTerminal(rendered string, width, height int) string {
	contentWidth := lipgloss.Width(rendered)
	contentHeight := lipgloss.Height(rendered)

	if width <= 0 && height <= 0 {
		return rendered
	}
	if width < contentWidth {
		width = contentWidth
	}
	if height < contentHeight {
		height = contentHeight
	}

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, rendered)
}
