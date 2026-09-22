package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
	"github.com/joaoseixas88/lazylock/internal/tui"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	program := tea.NewProgram(tui.New(ctx, fake.DemoCatalog()), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazylock:", err)
		os.Exit(1)
	}
}
