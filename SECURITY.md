# Security policy

## Supported versions

Surface is pre-1.0 software (tagged `0.x`). Security fixes are made on `main` and released as a new patch version. There are no backports.

## Report a vulnerability

Do not include an exploit, token, credential, database or other sensitive material in a public issue.

Use GitHub's private vulnerability-reporting flow when the repository's Security tab offers it. If it is unavailable, contact a repository maintainer privately through a contact channel published on the Hollis Labs organization or maintainer profile. Include:

- the affected commit and operating system
- the part involved (the page, `/api`, `/mcp`, a module)
- whether it was reached directly on loopback or through a proxy
- reproduction steps and the security impact
- a safe way to contact you about coordination

Maintainers will acknowledge a private report, investigate it, and coordinate disclosure; response times are best effort.

## Deployment boundary

## Deployment boundary

A Surface app is a single-owner application for one machine.

- **Loopback only.** The app refuses to serve on a non-loopback address. It has no sign-in: whoever can reach the port is the owner.
- **Its own page only.** A request that changes something and carries an `Origin` must come from the app's own page (or the local dev server).
- **Agent tools.** The embedded agent asks the owner before a tool that changes data runs. The MCP and HTTP tool endpoints do not ask: they are for the owner's own scripts and agents on the same machine.
- **Secrets.** The agent's model key is read from a file named in the agent's config file; it is never stored in the app's database and never returned by an API.

Do not put a Surface app behind a public proxy.
