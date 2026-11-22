# Sign-in

A Surface app serves one owner on loopback by default. Set `auth: identity` in its `surface.yaml` and people sign
in instead, through Substrate's identity module (users, sessions, roles, API keys; Substrate's
`docs/identity.md`), and the app may serve beyond loopback.

```yaml
app: myapp
title: My App
addr: 127.0.0.1:8190
auth: identity        # off (default) | identity
modules: [ ... ]
```

`surface generate` then adds Substrate's `auth` (mode `enforce`) and `identity` modules and an `identity` block
(`key_file` in the data directory, `setup: offline`).

## First run

Nobody can claim the app from an open page. On the host, make a single-use setup link and open it:

```sh
myapp identity setup-link --url http://<where the app is reached>
```

The page shows a form that creates the owner and signs them in. The link works once and expires (30 minutes by
default). Until an owner exists, the page says how to make the link. `myapp identity setup` (password on stdin)
does the same with no browser.

## What is guarded

- Substrate serves the identity routes (`/api/setup`, `/api/login`, `/api/logout`, `/api/password`,
  `/api/token`, `/api/pair`, `/api/identity/…`) behind its own guard.
- Every other `/api` and `/mcp` request needs a credential: the session cookie from the page (HttpOnly,
  SameSite=Strict), an API key, or a token minted from either. Reading needs `<app>.read`, changing anything
  `<app>.use`. Roles grant these: owner (everything), admin, member (read and use), guest (read).
- A cookie-authenticated write must carry `X-Requested-With` and this app's `Origin`; a cookie-authenticated
  WebSocket handshake needs this app's `Origin`. The page's fetch does this for every call, including modules'.
- The page and its assets are served to anyone, so the sign-in can draw. Module routes marked
  `module.Public` are served without a caller (the module authenticates them itself).
- Loopback is not trusted: agents run on the same machines. The CLI (`myapp desk`, `myapp call …`) signs with an
  operator key it makes on first use and keeps (0600) in the data directory as `operator.key`; whoever can run
  the binary against the data controls the app anyway.

## In a module

`module.UserOf(ctx)` is the signed-in caller (`ID`, `Kind` user or service, `Username`, `DisplayName`, `Role`,
`Scopes`, `HasScope`). Stamp what a person creates with their id; gate admin actions on
`HasScope("<app>.admin")`. See writing-a-module.md, "Routes, events, marks". A module that gives an outside process a
credential mints an identity API key with `module.KeysOf` (create, revoke, verify), never a token store of its
own.

## In the page

`mountPage({ modules, app })` (pass the generated `app` export) draws the gate: setup from a link, sign-in,
"set up on the host", and the desk once signed in, with an account menu in the bar (change password, sign out).
A 401 on any call returns to sign-in.

## HTTPS and passkeys

Passkeys need a secure context: HTTPS, or `http://localhost`. Three environment settings (an installer or a
service unit sets them) cover the two ways to get there:

| setting | meaning |
|---|---|
| `<APP>_PUBLIC_URL` | where people reach the app, e.g. `https://app.example.com`. Its origin is accepted for sign-in, an https one makes the session cookie `Secure`, its host is the passkey relying party, and the app answers to that host name. |
| `<APP>_TLS_CERT`, `<APP>_TLS_KEY` | serve HTTPS directly with this certificate and key (PEM). They are read again when the files change, so a renewal needs no restart. |
| `<APP>_TRUSTED_PROXIES` | comma-separated CIDRs of a TLS-terminating proxy in front (a tunnel, `tailscale serve`, Caddy); only their `X-Forwarded-Proto` is believed. `127.0.0.1/32` for one on the same host. |

- **Serving HTTPS itself**: set the certificate and key, and `PUBLIC_URL` to the certificate's name
  (`https://app.example.com:8470`).
- **Behind a proxy**: keep the app on loopback, point the proxy at it, set `PUBLIC_URL` to the proxy's https
  address and `TRUSTED_PROXIES` to where the proxy connects from.

With a secure `PUBLIC_URL` the page offers **Sign in with a passkey** beside the password form (shown only in a
secure context), offers to add one right after the owner is created, and the account menu's **Passkeys** adds,
lists and removes them and turns on **passkey-only** (the password stops working). People can always start with a
password and move up when they are ready: password, then a passkey, then passkey-only.

## Not yet

Invites and a users page, TOTP.
