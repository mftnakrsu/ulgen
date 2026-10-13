# Security

## Reporting a problem

Please do not open a public issue for a security problem. Report it privately
through GitHub: the **Security** tab of this repository, then **Report a
vulnerability**. Reports in Turkish or English are welcome.

Never include a real EVREN key in a report. If you believe a key has leaked,
revoke it on EVREN's API keys page first.

You will get an answer as soon as the maintainer can give one; this is a
volunteer project and promises no response time.

## What counts

Anything that breaks one of these:

- The key is not printed, logged, written into another program's file or put
  into an error message, and is sent only to EVREN (or to the address the user
  gave with `--base-url`).
- `ulgen setup` does not damage or silently overwrite a user's settings file.
- `ulgen run` does not start a tool in a project whose settings can send the
  key elsewhere, unless `--trust-project` is given.
- Arguments passed to a tool cannot run another command.
- The translator listens only on this machine and accepts only its own token.
- A worker cannot write outside its directory or read the key places.
- The install scripts install only a file whose checksum matched.

`docs/design.md` describes each of these in more detail.

## Supported versions

The latest release.
