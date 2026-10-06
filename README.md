# LazyLock

A keyboard-driven TUI for reading secrets across providers, starting with
Infisical.

## Current state

LazyLock reads a real Infisical instance: projects, environments, folder paths,
and the secrets at each path, with values hidden until you ask for them. Secrets
imported from other paths are included and say where they come from, so what
you copy or export matches what `infisical run` would inject. It can also change
secrets, always after a review you confirm; `-readonly` starts it unable to
change anything.

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
| `[` / `]` | anywhere | previous / next environment, keeping the path or its closest parent |
| `enter` | Projects, Paths | focus the next pane |
| `enter` | Secrets | details: the whole value, comment, tags, version and origin |
| `enter` | Actions | run the selected action |
| `/` | Projects, Paths, Secrets | filter by project name, folder path or key; never by value |
| `space` / `a` | Secrets, details, comparison | reveal the selected value / every value |
| `y` / `Y` | Secrets, details | copy the value / copy `KEY=value` lines |
| `v` / `V` | Secrets | mark the selected secret / mark or unmark every visible one |
| `x` | anywhere | export the marked secrets, or the whole path |
| `c` | anywhere | compare the path with the same path in another environment |
| `n` | anywhere | new secret in the selected path |
| `e` | Secrets, details | edit the selected secret's value, comment or key |
| `d` | Secrets | delete the selected secret, or the marked ones |
| `o` | anywhere | open the path in the Infisical web UI |
| `r` | panes, comparison | reload |
| `L` | anywhere | log out |
| `esc` | anywhere | close a dialog, then clear the filter, then the marks |
| `?` | anywhere | list every key |
| `q` / `ctrl+c` | anywhere | quit; `ctrl+c` also works inside dialogs |

The paths pane shows one environment at a time, its folders as a tree, with
the environments as tabs in its title. Switching to another instance lives in
the Actions menu; the current session
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

## Writing

Every write shows what will change and waits for `y`; `enter` never confirms.
In an environment whose slug or name contains `prod`, you then type its name.
Under an Infisical approval policy a write opens a change request instead, and
the footer says nothing has changed yet. If the connection drops mid-write,
LazyLock says it cannot tell whether the write went through and reloads,
rather than trying again. Imported secrets are changed where they live, so
editing or deleting one is refused with its origin.

The editor has fields for the key, the value and the comment: `tab` moves
between them, `enter` adds a line to the value, `ctrl+s` goes to the review, and
`esc` asks before throwing changes away. A new key may use letters, digits, `_`
and `-`; creating one that is only imported says it will hide the import.

Editing starts from the value as stored, so a `${...}` reference stays a
reference, and only the fields you change are sent. A value the editor could not
hand back unchanged (tabs, carriage returns), or one you cannot read, stays
locked and is never sent unless you replace it with `ctrl+r`. Just before
saving, LazyLock checks the secret's version; if someone else changed it after
you opened it, the review comes back with what it holds now and asks again.

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

Copying secrets between environments is not implemented yet; `C` is kept for
it. Neither is a second
provider, though the `domain.Catalog` port exists so one can
be added without touching the TUI.
