# Installing Ülgen

## The install scripts

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.ps1 | iex
```

Both download the archive for the machine and the processor, check its
SHA-256 against `checksums.txt` before unpacking anything, and put the binary
in the install directory: `$HOME/.local/bin`, or
`%LOCALAPPDATA%\Programs\ulgen` on Windows. They never run `sudo`, never ask
for a password and write nowhere else. On Windows the directory is added to
your own `PATH`, not the machine's.

| Variable | Meaning |
|---|---|
| `ULGEN_VERSION` | The release to install, for example `1.2.3`; the latest when unset |
| `ULGEN_INSTALL_DIR` | Where to put the binary |
| `ULGEN_BASE_URL` | Where the release files live, for a mirror. Only `https://`, or `http://127.0.0.1:PORT` and `http://localhost:PORT` on the same machine. A mirror needs `ULGEN_VERSION` |

`install.ps1` takes `-NoPathUpdate` to leave `PATH` alone.

## Pardus, Debian and Ubuntu

Each release carries `ulgen_<version>_amd64.deb` and `ulgen_<version>_arm64.deb`
(and the same as `.rpm` for Fedora and its relatives; install one with `sudo dnf install ./ulgen_<version>_amd64.rpm`).
`install.sh --deb` downloads the one for this machine, checks it, leaves it in
the current directory and prints the line that installs it:

```sh
curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh -s -- --deb
sudo apt install ./ulgen_<version>_amd64.deb
```

## From source

Go 1.26 or newer:

```sh
go install github.com/mftnakrsu/ulgen/cmd/ulgen@latest
```

## Checking a download by hand

`checksums.txt` in a release lists the SHA-256 of every file, and
`checksums.txt.sigstore.json` is its signature. The release workflow makes the
signature with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/),
using the workflow's own identity; there is no key of ours that could leak.

First that the checksum file was made by this repository's release workflow:

```sh
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/mftnakrsu/ulgen/\.github/workflows/release\.yml@refs/tags/v' \
  checksums.txt
```

Then that the file you downloaded is the one it lists:

```sh
sha256sum -c checksums.txt --ignore-missing   # or: shasum -a 256 -c checksums.txt --ignore-missing
```

On Windows, `Get-FileHash .\ulgen_1.2.3_windows_amd64.zip -Algorithm SHA256`
must equal the line for that file.

The install scripts do both. The checksum is always checked. The signature is
checked when `cosign` is installed, and then a release without a valid one is
not installed; without `cosign` the scripts say that the signature was not
checked and go on.

## Removing Ülgen

```sh
ulgen setup --remove all   # undo what Ülgen wrote into the tools
ulgen logout               # remove the saved key
```

Then delete the binary. `--remove` takes out only what is Ülgen's (its
entries, a default that pointed at them, the profile and the skill file it
installed) and leaves what you wrote; every file it changes or removes is
backed up first, under `backups` in Ülgen's configuration directory, which you
can delete once you no longer want the backups.
