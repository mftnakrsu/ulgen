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

## The documentation site

<https://ulgen.mefby.com> is built from `docs-site/` with Astro Starlight and
published from `main`. Each page is in `docs-site/src/content/docs/`, and its
Turkish translation is at the same path under `tr/`; the long Turkish guide,
`tr/evren-ile-kodlama-araclari.md`, has no English version. The "Using Ülgen"
page says what `README.md` says from "First run" to "Known limits", and its
Turkish translation what `README.tr.md` says, so a change to those sections
goes into the page as well. To see the site:

```sh
cd docs-site && npm ci && npm run dev
```

`npm run build` builds it as CI does.

## Correcting a translation

Ülgen's Turkish messages are in `internal/i18n/locales/tr.json`, and the
site's Turkish pages in `docs-site/src/content/docs/tr/`.

## Licence

Contributions are accepted under the project's licence, AGPL-3.0-only.
