# Handoff iOS

Native SwiftUI client for Handoff. iOS 17+, no dependencies — URLSession + Codable + async/await. Signs in as a person with the `handoff_session` cookie (same as the web app), never an agent bearer token.

## Build

Open `Handoff.xcodeproj` in Xcode 16+, pick a simulator or device, Run. Or:

```
xcodebuild -scheme Handoff -destination 'platform=iOS Simulator,name=iPhone 16' build
```

## Sign in

1. Enter the workspace host, e.g. `acme.example.com` — or `acme.handoff.localhost:8080` against a local dev server (`make dev`; `*.localhost` resolves in the Simulator too). Plain `http` is used for `localhost`/`127.0.0.1`, `https` everywhere else.
2. The app opens the workspace's web sign-in in a `WKWebView`. Magic links open in Safari by design, so on a real device copy the emailed link and paste it into the "Paste your emailed sign-in link" field — the app loads it in the web view and captures the `handoff_session` cookie.
3. On a dev server (`HANDOFF_DEV=1`) just tap the "open the sign-in link" dev shortcut in the web view.

The session cookie is stored in the Keychain and sent as a `Cookie` header on every call; all non-GET `/api` calls carry `X-Handoff: 1`. A 401/403 response returns to the sign-in screen. Sign out clears the Keychain and the web view's cookies.

## What's here

- `HandoffApp.swift` — app entry, `Session` (client, /api/me, Keychain restore, sign out), root switcher.
- `API.swift` — `Client` (request/tool helpers), Codable models, `Keychain` store.
- `SignInView.swift` — workspace host form + `WKWebView` sign-in with cookie capture.
- `TasksView.swift` — Open / Ready / Mine lists, pull-to-refresh, agent-working + claim dots.
- `TaskDetailView.swift` — meta, tappable todos (`update_todo`), comments (`comment`), claim/release/close.
- `NewTaskView.swift` — title + optional project/due (`create_tasks`).

`Info.plist` allows plain-http loads only for `localhost`/`handoff.localhost` subdomains.
