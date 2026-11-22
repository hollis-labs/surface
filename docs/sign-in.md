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
`HasScope("<app>.admin")`. See writing-a-module.md, "Routes, events, marks".

## In the page

`mountPage({ modules, app })` (pass the generated `app` export) draws the gate: setup from a link, sign-in,
"set up on the host", and the desk once signed in, with an account menu in the bar (change password, sign out).
A 401 on any call returns to sign-in.

## Not yet

Passkeys and HTTPS (the app serving TLS itself, or behind a TLS proxy), invites and a users page, TOTP.
