# Verification — 2026-09-21

## Automated checks

- `go test -race ./...`: passed. Authentication, CSRF/origin enforcement,
  logout revocation, two-user playlist ownership, playlist create/update/delete
  write-through, user settings isolation, encryption at rest, reopening the
  settings store with its persisted key, track-state validation, media Range
  requests, cover art, endpoint allowlisting, and radio URL/PLS/M3U handling.
- `go vet ./...`: passed.
- `tests/run-browser.sh`: passed in Chromium using Playwright 1.63.0. Login,
  square/loaded album covers, actual HTMLAudioElement playback of a generated
  WAV fixture, favorites, queue movement, playlist creation/rename/track removal/
  deletion, search, installed preset order, station persistence on reload,
  logout, second-user isolation, and desktop/mobile layout. No JavaScript errors.
- All Go files remain below the 300-line ceiling. Production uses the Go standard
  library and static browser modules without an npm build or runtime dependency.
- Fresh and existing data directories bootstrap the local-only `weazladmin` /
  `admin` account when that account is absent. The default password can be
  changed through the account dialog; it is never used as a Navidrome credential.
- Admin installation settings are encrypted at rest and protected from
  non-admin sessions. The admin can save the shared Navidrome URL and LLM
  provider configuration without exposing API keys to the browser.
- Normal users authenticate directly against the configured Navidrome server;
  the app stores only the generated API token and salt. New Navidrome users are
  provisioned on first successful login, and playlist writes use their upstream
  owner identity. The login surface has no local registration control.
- Radio acceptance remains green: all installed presets render in order, radio
  playback relays through same-origin routes, ICY metadata updates the player,
  user stations persist, and private/link-local radio targets are rejected.

## Container

`docker compose up --build -d` successfully builds and starts the application.
The service publishes `0.0.0.0:4000`, runs as UID/GID 10001, and uses a named volume
for `/data`, with an init process and a 15-second graceful stop window. Direct HTTP and the health endpoint return 200. The filesystem is
read-only except for the data volume and temporary directory. The application
requires no public-domain setting or bundled reverse proxy.

## Existing live services

Using the Navidrome connection already configured in Subweazl, verified:

- Login and logout through the app, without printing or saving credentials.
- Four recent albums, one album's 13 tracks, and ten existing playlists.
- Real JPEG cover art returned through the authenticated media route.
- Installed preset entries (private station subsequently removed from bundled defaults).
- DEF CON Radio PLS resolution and 8,192 bytes of MP3 audio through the relay.
- Live SomaFM directory (46 results) and Icecast directory (100-result page).

No real Navidrome playlist was created, edited, or deleted during verification.
Playlist mutations were tested against an isolated Navidrome fixture, with
separate Alice and Bob accounts. User acceptance should include creating a small
playlist in the web app and seeing it in that same user's Navidrome interface.

## Remaining acceptance boundaries

The user's reverse-proxy configuration is outside this app and has not been
changed or tested. Browser playback of the user's entire codec collection,
all external stations, multi-browser compatibility, concurrency/load,
and long-run power use are not claimed as verified. The first release does not
include HLS, bundled transcoding, or cross-device
queue coordination. See the README for deployment and operational details.

## Revised account flow and scrolling

After the first smoke test, local web-app accounts replace direct Navidrome login.
New integration checks prove that local registration/login do not contact
Navidrome; passwords differ between local and upstream identities; saved
connections survive an application restart; failed connection tests preserve the
previous valid connection; and different local users can choose different servers.
Playlist writes use the configured upstream owner, not the local username.
Previous settings migrate only after the matching upstream credentials verify.

Browser checks now register local accounts, configure Navidrome through the UI,
then exercise the existing playback and playlist workflow. They also verify
horizontal recent-album scrolling and sidebar scrolling on a short viewport.
The redundant sidebar playlist list has since been removed. The first-release
live-service checks above describe the original test run, not automatic server
configuration in the revised app. Deployment no longer accepts NAVIDROME_URL.

## Radio, Mood, and random playback revision

Go tests cover ICY stripping without audio corruption, metadata/session isolation,
stream cleanup, both Ollama NDJSON and vLLM SSE, fragmented IDs, duplicate/invented
ID rejection, 20-track enforcement, same-owned-playlist reuse, immediate Navidrome
write-through, settings/key isolation, concurrent build rejection and cancellation.
Browser tests cover curator settings/model discovery, incremental Mood rendering,
unchanged current playback, random play with 30 queued tracks, the Weazl favicon,
radio title/artist rendering and metadata connection cleanup when paused.
A bounded live read of the installed 80s/90s station returned ICY metadata for
Michael Jackson — Billie Jean. LLM generation is tested with isolated streaming
fixtures; the user's actual model endpoint remains a smoke-test boundary.
