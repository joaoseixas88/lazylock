package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain"
	"github.com/joaoseixas88/lazylock/internal/tui"
)

func main() {
	program := tea.NewProgram(tui.New(domain.DemoCatalog()), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazylock:", err)
		os.Exit(1)
	}
}
