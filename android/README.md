# Handoff Android

Native Kotlin + Jetpack Compose client for Handoff. Signs in as a person
(`handoff_session` cookie) against the same HTTP API the web app uses —
workspace host + magic link, or the `dev_link` fast path in dev mode.

## Build

Requires Android SDK (compileSdk 35) and JDK 17+. From this directory:

```sh
./gradlew assembleDebug   # APK at app/build/outputs/apk/debug/app-debug.apk
./gradlew test            # unit tests
```

## Sign in

Enter the workspace host, e.g. `https://acme.handoff.app` (a scheme is
required; `acme` alone won't work). Email field triggers the magic-link flow;
in `HANDOFF_DEV=1` mode the `dev_link` response is redeemed immediately.
For real magic links, copy the emailed link and paste it into the
"paste your sign-in link" field — it redeems the token natively and stores
the cookie. "Or sign in with the web flow" opens an in-app WebView (covers
the browser sign-in UI; CookieManager captures `handoff_session`).

## Dev server + emulator

`adb reverse` doesn't help because orgs live on subdomains. Simplest path:
run a single-org server (ignores host routing) whose base URL is the
emulator's host alias, so magic links/dev_link point somewhere reachable:

```sh
make dev-db
HANDOFF_SINGLE_ORG=<slug> HANDOFF_ADDR=:8081 \
  HANDOFF_BASE_URL=http://10.0.2.2:8081 HANDOFF_DEV=1 bin/handoff serve
```

On the emulator use host `http://10.0.2.2:8081`. Debug builds allow
cleartext only for loopback/`10.0.2.2`/`*.localhost` (see
`app/src/debug/res/xml/network_security_config.xml`). If the dev_link host
is `*.localhost` instead, `adb reverse tcp:8081 tcp:8081` reaches the host
server too.

## Notes

- Cookie is stored in EncryptedSharedPreferences and sent as `Cookie:` on
  every request; all non-GET calls send `X-Handoff: 1`.
- 401 responses sign you out and return to the sign-in screen.
- No push notifications, no Google OAuth shortcut (use web flow), no SSE
  live updates — pull-to-refresh manually.
