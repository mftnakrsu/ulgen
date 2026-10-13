# Contributing to Ülgen

Thank you for looking. Bug reports, corrections to the Turkish texts and code
are all welcome. Issues and pull requests may be written in Turkish or English.

## Building and testing

Go 1.26 or newer; nothing else for the program itself.

```sh
go build ./...
go vet ./...
go test ./...
gofmt -l .        # must print nothing
```

The tests never touch your keychain, your home directory or the network, and
they start no real tool.

CI runs all of this on Linux, Windows and macOS.

## What a pull request needs

- One change per pull request.
- A test for a change in behaviour. For a bug, a test that fails without the fix.
- Messages a user sees go into both `internal/i18n/locales/en.json` and
  `tr.json`; a test keeps the two in step.
- No new dependency without a reason the standard library cannot meet.
- `docs/design.md` updated when a decision recorded there changes.

## The key

Ülgen's first promise is that the key does not leak. A change must not print
the key, log it, write it into another program's file or put it into an error
message. The tests check every command's output for the test key; keep new
commands inside that check.

Never paste a real key into an issue, a log or a test.

## Correcting a translation

Ülgen's Turkish messages are in `internal/i18n/locales/tr.json`.

## Licence

Contributions are accepted under the project's licence, AGPL-3.0-only.
