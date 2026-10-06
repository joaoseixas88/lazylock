package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joaoseixas88/lazylock/internal/clipboard"
	"github.com/joaoseixas88/lazylock/internal/config"
	"github.com/joaoseixas88/lazylock/internal/credstore"
	"github.com/joaoseixas88/lazylock/internal/domain/fake"
	"github.com/joaoseixas88/lazylock/internal/tui"
)

func main() {
	demo := flag.Bool("demo", false, "browse built-in sample data instead of a real instance")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Nothing here touches the network or the keyring: reading the keyring can
	// block on a desktop unlock prompt, and the TUI can say so while it waits.
	model := tui.New(ctx, fake.DemoCatalog())
	if !*demo {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "lazylock:", err)
			os.Exit(1)
		}
		model = tui.NewApp(ctx, cfg.WithEnvOverrides(), credstore.New())
	}
	model = model.WithEffects(tui.Effects{Copy: clipboard.New(os.Stdout).Copy})

	if _, err := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazylock:", err)
		os.Exit(1)
	}
}
