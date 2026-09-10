# WeazlTunes web delivery plan

## Decisions

- Preserve the approved UI and `weazlhead` branding. Keep `mockup-ui/` as the design reference.
- Port 4000 for development and containers. Container listens on `0.0.0.0`.
- One Go process serves static assets, API, and authenticated media. No Node runtime.
- Users create/sign in to a local web-app account, then configure their own
  Navidrome URL and credentials. Local accounts and upstream identities are separate.
- Navidrome is the source of truth for library, favorites, and playlists. Every
  playlist write uses the configured Navidrome user's credentials, never a shared admin.
- One browser audio element. Radio pauses the library without discarding its queue.
- Imported Weazltunes presets seed each user's radio settings in original order.

## Phase 1 — Deployment foundation

Go HTTP server, embedded assets, configuration validation, health check, graceful
shutdown, multi-stage non-root Docker image, persistent data volume, Compose,
direct HTTP access behind a user-managed reverse proxy. Exit: local and container health checks pass on port 4000.

## Phase 2 — Identity and library

Local registration/login, encrypted saved Navidrome connections, opaque HttpOnly sessions, expiry/logout, mutation
origin checks, login limits, album pagination, search, cover art, favorites.
Exit: two users stay isolated; anonymous access is rejected; upstream failures
are actionable without exposing credentials.

## Phase 3 — Playback and Navidrome playlists

Authenticated streaming with Range support, one audio element, seek/volume,
queue controls, saved queue, playlist create/read/rename/add/remove/delete.
Server checks playlist ownership before mutation; writes complete on Navidrome
before success is shown. Exit: fake-upstream integration tests prove owner
identity and write-through; browser exercises the full flow.

## Phase 4 — Radio

Real installed presets, per-user saved stations, eight preset slots, PLS/M3U
resolution, same-origin radio relay, on-demand SomaFM/Icecast directories.
Outbound radio fetching rejects private/link-local destinations and validates
redirects at dial time. Exit: playlist resolution and URL-policy tests pass;
UI switches sources while preserving the library queue.

## Phase 5 — Release checks

Race tests, browser checks, Docker build/run, persisted state after restart,
reverse-proxy deployment notes and operational limits. Live Navidrome acceptance
requires a configured server/user; tests must not alter existing real playlists.

## Later, after the core is in use

Library metadata cache, queue conflict handling across devices, and explicit
transcoding policy.
No background LLM work or decorative visualization.

## Delivery status — 2026-09-10

Phases 1–5 are implemented. The app runs in Docker on `0.0.0.0:4000` with
local web-app authentication and per-user Navidrome connections. The approved mockup remains under `mockup-ui/`.

Go race tests cover authentication/origin checks, playlist write-through and
ownership, encrypted settings and user isolation, media byte ranges, and radio
URL policy. Browser tests exercise playback and the complete playlist workflow
against an isolated fixture. Docker build and direct HTTP health checks pass.
Live Navidrome login, browse, covers, and playlist reads were verified; the first
installed radio preset successfully delivered audio through the relay. Real
playlist mutations are reserved for the user's smoke test.

Implementation choice: encrypted atomic per-user files replace a SQLite dependency
for the small queue/radio settings store. Navidrome owns the large library and
playlist data; local session tokens stay in memory and expire on restart.

The first smoke-test revision separates app login from upstream credentials,
removes NAVIDROME_URL from deployment, makes all sidebar playlists scrollable,
and adds horizontal browsing of 16 recent albums.

The next revision adds on-demand per-user Ollama/vLLM Mood curation with streaming
Navidrome writes, ICY now-playing metadata, Weazl favicon, and random playback
with 30 upcoming tracks. The duplicate sidebar Mixtapes surface is removed.
