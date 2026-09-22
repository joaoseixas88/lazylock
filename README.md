# LazyLock

A keyboard-driven TUI for reading secrets across providers, starting with
Infisical.

## Current state

LazyLock reads a real Infisical instance: projects, environments, folder paths,
and the secrets at each path, with values hidden until you ask for them. It is
read-only — nothing it does can change a secret.

```sh
go run ./cmd/lazylock
```

On first run it asks for your instance URL (self-hosted or
`https://app.infisical.com`), then opens your browser to log in, exactly the way
`infisical login` does. If the browser cannot reach LazyLock — a remote session,
or a browser on another machine — the login screen also accepts the token the
Infisical page shows you.

The session is kept in your OS keyring, and falls back to a `0600` file under
`$XDG_STATE_HOME/lazylock/` only when no Secret Service is running. The footer
tells you which of the two is in use. There is no refresh token, because
Infisical never hands one to a CLI; when the session expires you log in again.

To look around without an instance:

```sh
go run ./cmd/lazylock -demo
```

## Keys

`1` Projects, `2` Paths / Environments, `3` Actions, `4` Secrets. Arrows or
`j`/`k` move inside the focused panel, `space` reveals the selected secret, `r`
reloads the focused panel, and `q` quits. A value is hidden again whenever the
selection changes. A secret you have permission to list but not to read says so
rather than showing a blank.

## Configuration

`~/.config/lazylock/config.json` holds the instance URL and the last account.
Environment variables override it and are never written back:

| Variable | Effect |
|---|---|
| `LAZYLOCK_SITE_URL` | instance URL, with or without a trailing `/api` |
| `LAZYLOCK_TOKEN` | use this JWT directly and skip both the keyring and login |
| `LAZYLOCK_CONFIG` | path to the config file |
| `LAZYLOCK_SESSION` | path to the session file, when the file fallback is in use |

## Roadmap

Writing, export, clipboard, multi-select and environment-to-environment copy are
not implemented. Neither is a second provider, though the `domain.Catalog` port
exists so one can be added without touching the TUI.
