# Handoff desktop

A thin Electron shell around a hosted Handoff workspace, the same idea as the
Slack or Notion desktop apps. The server does everything; the app just loads
`https://<slug>.<your-domain>` in a window, keeps you signed in, and sends
outside links to your real browser.

## What's inside

- **First run** asks for your workspace address and saves it (in Electron's
  `userData/config.json`). **File → Switch Workspace** clears it and asks again.
- **Sign-in persists**: the window uses a persistent session partition, so the
  `handoff_session` cookie survives restarts.
- **Outside links open in the system browser** (`setWindowOpenHandler` +
  `will-navigate`/`will-redirect` guards). The workspace host, its parent domain
  (the apex, where signup and the shared auth callback live) and
  `accounts.google.com` stay in the window so Google sign-in works.
- Standard Edit/View/Window menus (reload + DevTools in dev), 1000×700 min size,
  single instance.
- **Unread badge** on macOS/Linux docks, polled from the same `inbox` endpoint
  the web app uses.

## Run it in development

```sh
cd desktop
npm ci
npm start
```

Point it at a local dev server too — e.g. `http://acme.handoff.localhost:8080`
(see "Run it locally" in the root README).

## Build installers

```sh
cd desktop
npm ci
npm run dist          # Windows + macOS + Linux
npm run dist:win      # just Windows: NSIS installer + portable .exe
npm run dist:mac      # just macOS: .zip
npm run dist:linux    # just Linux: AppImage
```

Artifacts land in `desktop/release/`.

**What Linux can build:** the Windows NSIS installer + portable exe and the
Linux AppImage build fine on Linux; the macOS `.zip` builds but an unsigned zip
still triggers Gatekeeper on macOS (users right-click → Open). A `.dmg` needs
macOS tooling, so it isn't configured.

**Unsigned builds.** Nothing here is code-signed or notarized — no certs are
wired in. Windows will show SmartScreen and macOS will warn on first open.
Signing, notarization and a real dmg are a follow-up once Apple/Windows
credentials exist; that's also when a proper app icon should land (the builds
currently ship the default Electron icon).
