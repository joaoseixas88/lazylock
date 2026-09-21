# LazyLock

A keyboard-driven TUI for managing secrets and credentials across providers.

## Current state

The first milestone is a fully local prototype with deterministic mock data. It
does not connect to Infisical or store credentials.

Run it with:

```sh
go run ./cmd/lazylock
```

Use `1` for Projects, `2` for Paths / Environments, `3` for Actions, and `4`
for Secrets. Use up/down arrows or `j`/`k` to move inside the focused panel,
`space` to reveal the selected secret in Secrets, and `q` to quit. Secret values
are hidden by default and hidden again whenever the selection changes.

Panels fill the terminal width, with a single column between the left and right
panels and no blank rows between the left panels. Titles contain focus shortcuts;
the footer is left-aligned. On short terminals, content is clipped to preserve
titles and borders. Below 16 columns or 4 rows, a resize message is displayed.

## Roadmap

The next milestone will research and document the Infisical API and Go SDK,
then use that information to define the real provider adapter. CRUD, export,
clipboard support, multi-select, and environment-to-environment copy are not
part of this prototype.
