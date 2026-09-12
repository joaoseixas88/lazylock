# LazyLock

A keyboard-driven TUI for managing secrets and credentials across providers.

## Current state

The first milestone is a fully local prototype with deterministic mock data. It
does not connect to Infisical or store credentials.

Run it with:

```sh
go run ./cmd/lazylock
```

Use arrow keys or `j`/`k` to move, `enter` to open a context, `esc` to go
back, `space` to reveal the selected secret, and `q` to quit. Secret values
are hidden by default and hidden again whenever the selection changes.

## Roadmap

The next milestone will research and document the Infisical API and Go SDK,
then use that information to define the real provider adapter. CRUD, export,
clipboard support, multi-select, and environment-to-environment copy are not
part of this prototype.
