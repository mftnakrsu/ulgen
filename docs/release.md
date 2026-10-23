# Releases

This is the runbook for cutting a release of Ülgen. Installing one is described on the [install page](https://ulgen.mefby.com/install/).

## Cutting a release

1. Make sure `main` is green and holds what should ship. Push a version tag:

   ```
   git tag v1.2.3
   git push origin v1.2.3
   ```

2. The tag starts `.github/workflows/release.yml`. It checks the GoReleaser
   file, then runs `goreleaser release --clean`. GoReleaser builds six
   binaries (linux, darwin, windows on amd64 and arm64) and uploads, as a
   **draft** GitHub release: `ulgen_<version>_<os>_<arch>.tar.gz` (`.zip` on
   Windows), each with the binary, `LICENSE` and `README.md`; one `checksums.txt` with the SHA-256 of every file and its
   signature `checksums.txt.sigstore.json`; and `ulgen_<version>_<arch>.deb`
   and `.rpm` for `linux/amd64` and `linux/arm64`.
3. The release is a draft. Read it, then publish it by hand. Only a published
   release is public.

A run that is not a tag (a manual run, or a pull request touching
`.goreleaser.yaml`, the workflow or `scripts/install.*`) builds the same
files as a snapshot (`--snapshot --clean`) and uploads them as a workflow
artifact kept for seven days. A snapshot publishes nothing.

## Signing

The release job signs `checksums.txt` with cosign's keyless signing and
uploads `checksums.txt.sigstore.json` next to it. The signature is bound to
this workflow file at the tag (`.github/workflows/release.yml@refs/tags/v…`),
which is the identity the install scripts and the install page
(`docs-site/src/content/docs/install.md` and its Turkish translation,
`docs-site/src/content/docs/tr/install.md`) check. Moving the repository or
renaming the workflow changes that identity: the scripts and the pages have to
change with it.

A snapshot is not signed.

The release job installs cosign 2 (`sigstore/cosign-installer` 3), as for
v0.1.0, and Dependabot leaves that major version alone. cosign 2 writes the
older bundle format (`--new-bundle-format` is off by default), which every
cosign 2 and 3 reads.
cosign 3 (`cosign-installer` 4) writes the newer Sigstore bundle format and,
by default, a newer transparency log. cosign 2.6.5 reads such a bundle made
with a key, but the keyless form has not been checked, nor have the older
cosign 2 releases that Linux distributions package. Moving is its own change:
sign a test with cosign 3, then check it with the oldest cosign the install
scripts should accept.

v0.1.0 and v0.2.0 were signed this way. `install.sh` with cosign on the
`PATH` and the `cosign verify-blob` command on the [install page](https://ulgen.mefby.com/install/) both
accept their signatures, with cosign 2.6.5 and 3.1.3.

## The workflows' actions

Every action in `.github/workflows` is pinned by commit, with its version in
a comment (`actions/checkout@<sha> # v7.0.1`). A tag can be moved to other
code; a commit cannot. Dependabot (`.github/dependabot.yml`) proposes the new
commits once a week, as one pull request; check what changed before merging
it, since that code runs with the release's permission to sign and publish.
