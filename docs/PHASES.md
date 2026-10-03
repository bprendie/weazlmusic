# WeazlTunes web delivery plan

## Decisions

- Preserve the approved UI and `weazlhead` branding. Keep `mockup-ui/` as the design reference.
- Port 4000 for development and containers. Container listens on `0.0.0.0`.
- One Go process serves static assets, API, and authenticated media. No Node runtime.
- The local administrator configures one Navidrome URL; normal users sign in
  with their Navidrome credentials. The local admin identity remains separate.
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

## Phase 4 — Radio (complete)

Real installed presets, per-user saved stations, eight preset slots, PLS/M3U
resolution, same-origin radio relay, on-demand SomaFM/Icecast directories.
Outbound radio fetching rejects private/link-local destinations and validates
redirects at dial time. Exit: playlist resolution and URL-policy tests pass;
UI switches sources while preserving the library queue.

## Phase 5 — Release checks (next)

Race tests, browser checks, Docker build/run, persisted state after restart,
reverse-proxy deployment notes and operational limits. Live Navidrome acceptance
requires a configured server/user; tests must not alter existing real playlists.

## Later, after the core is in use

Library metadata cache, queue conflict handling across devices, and explicit
transcoding policy.
No background LLM work or decorative visualization.

## Delivery status — 2026-09-21

The deployment foundation, admin configuration, Navidrome identity handoff, and
radio surface are complete. The
app runs in Docker on `0.0.0.0:4000` with an encrypted persistent store,
local web-app authentication, a local-only `weazladmin` bootstrap account, and
direct Navidrome login for normal users.
The approved mockup remains under `mockup-ui/`.

Go race tests cover authentication/origin checks, playlist write-through and
ownership, encrypted settings and user isolation, media byte ranges, and radio
URL policy. Browser tests exercise playback and the complete playlist workflow
against an isolated fixture. Admin/non-admin settings protection, default-admin
password change, direct Navidrome login, upstream playlist ownership, Docker
build, and direct HTTP health checks pass.
Live Navidrome login, browse, covers, and playlist reads were verified; the first
installed radio preset successfully delivered audio through the relay. Real
playlist mutations are reserved for the user's smoke test.

Implementation choice: encrypted atomic per-user files replace a SQLite dependency
for the small queue/radio settings store. Navidrome owns the large library and
playlist data; local session tokens stay in memory and expire on restart.

The first smoke-test revision separates app login from upstream credentials,
removes NAVIDROME_URL from deployment, makes all sidebar playlists scrollable,
and adds horizontal browsing of 16 recent albums.

The next phase is release hardening: persisted upgrades, operational checks, and
reverse-proxy deployment notes. The local admin remains the escape hatch for
backend configuration while Navidrome remains the user identity source.

## Native API and Flight Recorder — 2026-10-03

The S0–S6 implementation is now present: durable independent native devices,
normalized music/media/presets/Mood, per-device queues, encrypted SQLite jobs,
recurrence/DST, bounded independent recording workers, immutable AAC segments,
scoped downloads and a compact web management/shared playback surface.
SQLite supplements the compatible encrypted legacy account/settings store;
the earlier standard-library-only/session-memory choices above describe history.

FDK AAC-LC160k/44100/stereo matches the user's iPod profile. One-minute synthetic
six-stream capture, real Chromium AAC playback, race/vet, restart and storage
checks are recorded in VERIFICATION.md. The six-hour soak is delegated to Luna
by the user; AVPlayer/background transfers require Apple acceptance. Capabilities
continue to report these two validation flags false. See SOL_WORKBOOK.md and
NATIVE_HANDOFF.md for current status, rather than the historical phase labels.
