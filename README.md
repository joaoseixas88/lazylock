# LazyLock

A keyboard-driven TUI for reading secrets across providers, starting with
Infisical.

## Current state

LazyLock reads a real Infisical instance: projects, environments, folder paths,
and the secrets at each path, with values hidden until you ask for them. Secrets
imported from other paths are included and say where they come from, so what
you copy or export matches what `infisical run` would inject. It is read-only —
nothing it does can change a secret.

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
Infisical never hands one to a CLI; when the session expires LazyLock deletes it
and goes back to the login screen.

To look around without an instance:

```sh
go run ./cmd/lazylock -demo
```

## Keys

| Key | Where | Action |
|---|---|---|
| `1` `2` `3` `4` | anywhere | focus Projects, Paths / Environments, Actions, Secrets |
| `↑` `↓` / `j` `k` | panes | move |
| `enter` | Projects, Paths | focus the next pane |
| `enter` | Secrets | details: the whole value, comment, tags, version and origin |
| `enter` | Actions | run the selected action |
| `/` | Projects, Paths, Secrets | filter by name, environment and path, or key; never by value |
| `space` / `a` | Secrets, details, comparison | reveal the selected value / every value |
| `y` / `Y` | Secrets, details | copy the value / copy `KEY=value` lines |
| `v` / `V` | Secrets | mark the selected secret / mark or unmark every visible one |
| `x` | anywhere | export the marked secrets, or the whole path |
| `c` | anywhere | compare the path with the same path in another environment |
| `o` | anywhere | open the path in the Infisical web UI |
| `r` | panes, comparison | reload |
| `L` | anywhere | log out |
| `esc` | anywhere | close a dialog, then clear the filter, then the marks |
| `?` | anywhere | list every key |
| `q` / `ctrl+c` | anywhere | quit; `ctrl+c` also works inside dialogs |

Switching to another instance lives in the Actions menu; the current session
stays stored, and `esc` on the next screen comes back to it. A secret you may
list but not read says so rather than showing a blank.

## Copying and exporting

Revealed values mask themselves again after 30 seconds, and whenever you move
to another path. Values are drawn with their control characters shown as
symbols, so a secret cannot drive your terminal.

`y` puts a value on the clipboard: `wl-copy --sensitive` on Wayland, so
clipboard history managers skip it, `xclip` or `xsel` on X11, `pbcopy` on macOS.
Inside an SSH session, or when none of those is installed, it goes to the
terminal's clipboard through OSC 52, which the footer reports as sent rather
than copied because the terminal never confirms it. The value is handed over on
stdin, never on a command line.

`x` writes `.env`, JSON or `.env.example`, to a file or to the clipboard. The
`.env` quoting matches Infisical's own, so the file reads back unchanged; JSON
is a flat `{"KEY": "value"}` object; `.env.example` keeps keys and comments
without values. Files with values are created `0600`, `.env.example` `0644`.
LazyLock asks before replacing a file, and before writing secrets inside a git
work tree where the file is not ignored. Secrets you cannot read are left out
and named.

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

Writing — creating, editing and deleting secrets, or copying them between
environments — is not implemented, and `e`, `d` and `n` are kept free for it.
Neither is a second provider, though the `domain.Catalog` port exists so one can
be added without touching the TUI.
