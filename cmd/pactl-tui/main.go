package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pablo/pactl-tui/internal/pactl"
	"github.com/pablo/pactl-tui/internal/ui"
)

func main() {
	client := pactl.New("pactl")
	program := tea.NewProgram(ui.New(client), tea.WithAltScreen())

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
