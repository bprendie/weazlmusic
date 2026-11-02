# WeazlMusic remote playback — server workbook for Sol

Created 2026-11-01. Inspected source baseline: `236a7c1`.
Status: **R0–R4 complete; owner accepted and production deployed 2026-11-02. GitHub publication approved 2026-11-02; receiver handoff remains pending.**

## Assignment and release sequence

Implement the WeazlMusic server protocol and browser controls for handing current
playback to an authenticated receiver. ShopWeazl is the first intended receiver;
Subweazl Android must be able to adopt the same protocol later.

The owner requested this sequence:

1. Prepare this workbook.
2. Sol implements and validates server/web changes against isolated receivers.
3. Review the result with the owner. Once accepted, deploy on the production
   machine and publish the corresponding code commits to GitHub.
4. After deployment/publication, create a separate ShopWeazl repository handoff
   for Codex to implement the receiver interface against the released contract.

This workbook authorizes no deployment or push by itself. Historical production
permissions in other workbooks concern earlier releases, not this review gate.
When assigned implementation, Sol should complete the implementation, tests,
documentation and concrete release candidate before requesting release approval.
Local reviewable commits are appropriate during implementation; preserve unrelated
work. Do not implement either native receiver or create its final handoff yet.

## Intended behavior and scope

The web player gains **Play on…**, listing **This browser** and available owned
receivers such as **ShopWeazl**. Selecting ShopWeazl transfers a snapshot of the
current library track, position and queue, or the current live station. The
receiver fetches media from WeazlMusic using its own native credentials/leases.
After confirmed startup, browser audio stops and browser controls target the
receiver. Closing or signing out of the controlling browser does not stop it.

Include in this release:

- Receiver registration, presence, capability discovery and revocation handling.
- Library queue transfer, including repeated tracks and approximate position.
- Live radio transfer at the live edge, through existing station/media routes.
- Remote play, pause, stop, previous, next, seek, volume and queue replacement.
- Remote enqueue and shuffle through versioned queue replacement; preserve
  existing browser entry points and physical-control state reporting.
- Explicit return to local browser playback, with honest failure handling.
- An isolated simulated receiver and reproducible contract/browser acceptance.

Defer synchronized multiroom playback, seamless sample-accurate transfer,
cross-account sharing, cross-installation transfer, arbitrary URL casting,
receiver-to-receiver atomic transfer, remotely waking sleeping phones, Chromecast/
AirPlay integration, Flight Recorder transfer, and direct-Navidrome remote control.
The browser is a controller/local source, not a remotely discoverable receiver in
this release. No changes to ShopWeazl, Android or iOS source are part of this task.

One target may have multiple authorized controllers; independent receivers may
play independently. This is not a single global queue for the entire account.
While controlling a remote target, returning to This browser is required before
transferring that playback to another target in v1. Explain this in the UI.

## Read first and evidence baseline

Read `README.md`, `docs/API_REFERENCE.md`, `docs/SUBWEAZL_API_V1.md`,
`docs/DEPLOYMENT.md`, `docs/VERIFICATION.md`, and applicable repository instructions.
Recheck the implementation baseline before starting; these observations describe
the inspected checkout, not a guarantee about production.

| Existing component | Reuse or integration constraint |
| --- | --- |
| `internal/server/v1_auth.go` | Native login/refresh/device revoke already exist. Device `lastSeen` records credential issuance, not receiver liveness. Native request identity has a background context, not automatic stream revocation. |
| `internal/server/v1_http.go` | Native envelopes, mutation UUIDs and ETags exist. Generic mutation handling holds `a.mu` across dispatch; do not hold it across stream writes, receiver waits or callbacks. |
| `internal/server/v1_store.go` | Encrypted SQLite records, single connection, lifecycle context and injected clock. Respect lock ordering and transaction boundaries. |
| `internal/server/v1_stations.go` | Native saved queues belong to the requesting device. Do not turn `/queue` into shared remote control or overwrite another device's saved queue implicitly. |
| `internal/server/v1_media.go`, `radio.go`, `radio_events.go` | Scoped media leases, range delivery and radio metadata. Retain authorization and radio origin protections. |
| `web/player.js`, `state.js`, `render.js`, `app.js` | Playback and many controls directly manipulate local `Audio` and queue state today. Audit all of them. |
| `web/api.js`, `device.js` | Browser cookie/CSRF and browser-local saved queue ID. Existing API helper expects legacy errors, not native error envelopes. |
| `web/mood.js`, `quick-actions.js`, `flight-playback.js` | Other entry points can mutate queues or start audio; output selection must apply consistently. |
| `cmd/weazlfixture`, `internal/testnav`, `internal/testradio`, `tests/` | Isolated server, fake library/radio and Playwright acceptance infrastructure. |

ShopWeazl was reachable at `http://10.0.0.42:3000` during planning. Its source at
`~/hardware/jay_tv/app/control.go` forwards commands to Subweazl's Unix socket.
`app/podcasts.go` already sends URL, metadata and position to the native audio
owner. The new receiver must later coordinate with that owner; do not contact or
change the live appliance as part of server tests.

Android's [PlaybackService](https://github.com/bprendie/subweazl_android/blob/main/app/src/main/java/io/prendie/subweazl/PlaybackService.kt)
uses Media3/ExoPlayer and persists queues. It is a future controller and receiver.
Capabilities must describe behavior, not platform names. Android background-start
restrictions mean a registered phone is not necessarily available; no wakeup
promise belongs in this contract.

## Architecture and enrollment

Use the existing Go service and data volume. Receivers initiate outbound HTTPS
connections to their configured WeazlMusic origin. Controllers use that same
server; neither the browser nor server opens connections to receiver LAN addresses.
Do not add LAN scanning, multicast discovery, inbound Pi ports or relaxed CORS.

Use **SSE for server-to-client events and authenticated HTTP mutations for commands,
reports and acknowledgements**. This matches existing deployment/proxy behavior
without requiring WebSocket support. Browser SSE uses same-origin cookies; native
SSE uses an HTTP client with bearer headers. Never put access tokens in event URLs.

Enrollment v1 is an ordinary native login to the same installation/account/library,
followed by explicit receiver registration. Existing refresh credentials handle
reconnect. “Pair once” initially means this enrollment, not a new short-code OAuth
flow. Do not copy controller cookies, passwords or tokens into command payloads.
Short-code pairing can be designed separately if desired later.

Scope every receiver, queue, command, report and event subscription by installation,
account and library identity. Server derives ownership from authentication. A
native device may register one receiver; its stable receiver ID can be its existing
device ID. Cookie clients may control owned receivers but cannot impersonate one.
Registration alone does not make a device online. Account roles do not grant
cross-user playback access. Reuse device revocation rather than inventing a second
independent credential store.

## Protocol contract to freeze before implementation

Create `docs/REMOTE_PLAYBACK_V1.md` as the normative implemented contract during R0.
The routes and shapes below are proposed defaults; Sol may refine them in R0,
recording the decision here. Freeze exact request/response/event/error fixtures
before browser and simulator implementation. Do not advertise unfinished support.

Add an independent `remotePlayback` extension capability with a revision, supported
source kinds, commands and limits. Preserve existing base contract behavior and
old clients; a missing capability means unavailable. Receiver advertisements are
intersected with server capabilities, never trusted to widen authorization.

All routes below are under `/api/v1/remote`:

| Method/path | Authentication and purpose |
| --- | --- |
| `PUT /receiver` | Native only. Register/update own display name and supported source kinds/controls. |
| `POST /receiver/connections` | Native only. Establish a receiver connection generation; return connection ID, server epoch and timing limits. |
| `GET /receiver/events?connectionId=…` | Native only. Own connection's snapshot/command/resync events. |
| `POST /receiver/reports` | Native only. Own generation's heartbeat and authoritative player report. |
| `POST /receiver/commands/{id}/ack` | Native only. Applied/rejected result for a delivered command, with resulting state. |
| `GET /players` | Cookie or bearer. Owned receiver metadata, presence, capabilities and last reported state. |
| `GET /players/{id}` | Cookie or bearer. Full owned target snapshot, queue and control revision. |
| `GET /events` | Cookie or bearer. Owned player/status/command changes; initial snapshot. |
| `POST /players/{id}/commands` | Cookie or bearer. Submit version-checked command; return 202 and command ID. |
| `GET /players/{id}/commands/{commandId}` | Cookie or bearer. Resolve queued/delivered/applied/rejected/expired/outcome-unknown status. |

Use existing native success/error envelopes. Controller mutations require UUID
`Idempotency-Key` and target control ETag in `If-Match`; missing/stale preconditions
follow existing 428/409 conventions. Repeated UUID/body/precondition returns the
same command, never another execution. Scope idempotency additionally to the
authenticated actor so two devices cannot collide using the same UUID.

Receiver registration/connection mutations use idempotency keys. Reports use
generation plus strictly increasing sequence, with identical retries accepted and
changed content at the same sequence rejected. Acks deduplicate by command ID,
generation and result; reject conflicting retries. Explicitly exempt these
high-frequency report/ack routes from generic 30-day mutation storage and document
their replacement semantics. No exemption may bypass auth/origin/body-size checks.

### Playback and queue model

Use typed references, not browser stream URLs. A library item has `entryId` (unique
queue occurrence) and `trackId`; duplicates of a track retain distinct entry IDs.
A live source has `kind: radioLive` and a stable owned `stationId`. The source
descriptor also carries installation/library identity as a consistency check,
never as permission. Positions/durations are integer milliseconds throughout.

Example command body (illustrative until R0 freezes fixtures):

```json
{
  "type": "load",
  "playback": {
    "kind": "library",
    "queue": [
      {"entryId": "occurrence-a", "trackId": "track-7"},
      {"entryId": "occurrence-b", "trackId": "track-7"}
    ],
    "currentEntryId": "occurrence-a",
    "positionMs": 42100,
    "playWhenReady": true
  }
}
```

Preserve queue order, occurrence identity and the current entry; do not deduplicate
by track ID. Browser conversion includes current plus upcoming entries. No repeat
mode is required initially: carry the browser's materialized shuffle order.
Receiver advances its loaded queue autonomously when the controller disconnects.
At end of queue it stops. Previous selects the preceding occurrence when available;
otherwise it seeks the current finite track to zero. Next at the end stops.

Separate the live station from a held library queue/resume position. Starting radio
must not discard that queue. Define resume-library behavior explicitly in R0;
default Next while radio is active resumes the held current library entry, or is
unavailable if there is none. Live radio has null position/duration and no seek.

Commands use explicit states (`play`, `pause`, `stop`, `seek`, `setVolume`,
`replaceQueue`, `load`), not retry-sensitive toggle operations. Define next/previous
as revision-checked operations applied once; stale local changes cause rejection.
`stop` halts output and retains a resumable snapshot. Volume is normalized 0..1
and affects only the selected receiver. Queue replacement specifies the retained
current occurrence/position and desired play state so enqueue does not restart
the current audio. Scrubbing/volume updates are coalesced by the controller.

Validate queue and resource shape, current-entry membership, uniqueness of entry
IDs, finite numeric bounds, source support and account/library access. Do not
resolve or lease 1,000 tracks synchronously under a global lock. Actual media access
still revalidates resource permissions as the receiver requests each lease;
unavailable media produces a truthful rejected/failed state, never fake success.

The receiver resolves metadata/artwork and obtains fresh leases with its own
credentials. Do not transfer expiring media leases, local file/cache paths or
upstream credentials. Local-only/offline references cannot be handed off. Browser
radio presets must resolve to existing stable station IDs; unsaved directory
stations require an explicit save action before transfer, not silent preset edits.
Keep the existing radio-origin/redirect rules, including explicit private origins.

### State, concurrency and presence

Keep desired commands separate from observed playback. HTTP 202 means accepted,
not audible. Report source, full queue when changed, current occurrence, transport
state (`idle`, `buffering`, `playing`, `paused`, `ended`, `error`), position,
duration, seekability, volume, last applied command and sanitized error.
Report metadata changes for live radio. Only the receiver reports applied state.

Use separate identifiers for server epoch, receiver connection generation, report
sequence and control revision. Position ticks/heartbeats must not invalidate every
controller command. Queue/track/transport/volume changes, including physical
buttons, increment the control revision. A receiver-local state revision accompanies
commands so a local change not yet reported can reject stale remote work. Freeze
the exact server/receiver revision mapping and ordering rules in R0. Include a
monotonic command sequence per generation so obsolete deliveries can be fenced
even after their deduplication history expires.

Serialize commands per receiver; allow only one unresolved command at a time in
v1, returning a documented busy conflict for competing requests. Controller UI
coalesces sliders and retries only after fetching current state. Never silently
overwrite another controller or physical-button update. Position-only seek commands
still participate in control ordering. Physical actions remain usable offline.
An uncertain command must not wedge control permanently: a fresh authoritative
report reconciles it, or a documented recovery stop fences earlier commands before
further work. Specify this escape path in R0, including delayed old deliveries.

Proposed bounded defaults, to publish in capabilities:

- Heartbeat every 10 seconds; offline after 30 seconds without a valid report.
  Online requires an attached command stream and a fresh complete state report.
- Position report every 2 seconds while playing, immediately for discrete changes.
  Controller interpolation stops when data becomes stale; do not claim precision.
- Command delivery/start deadline 15 seconds; never retain commands for hours and
  surprise a receiver when it reconnects. Distinguish proven nonexecution from an
  unacknowledged possible execution.
- Queue maximum 1,000 entries within existing 1 MiB request bound; bounded metadata.
- Maximum 16 registered receivers/account, 8 controller streams/account and one
  command stream/receiver; bounded event buffers, rate limits and reconnect backoff.
- Command status/idempotency retention 24 hours, with documented expired-history
  behavior and cleanup; telemetry in memory, not an ever-growing durable journal.

An SSE heartbeat alone is not proof the audio engine is healthy. Presence and
playback error are separate. Do not show the last known playing state as live when
the receiver is offline. A disconnect may leave audio playing; say “unreachable.”

### Delivery, restart, refresh and revocation

SSE is notification transport, not exactly-once execution. Send an initial snapshot
with a cursor, then ordered events. Retain a bounded replay window; an obsolete
cursor/server epoch produces an explicit resync snapshot. Snapshot/subscription
must not lose an update between reading state and attaching the stream. Overflow
disconnects/resyncs a slow reader without blocking the player or other users.

Receiver remembers applied command IDs/results for the protocol retry window and
checks deadlines/generations. Duplicate delivery returns the known result; it
must not repeat next/seek/load. A normal network/auth reconnect can resume the
same connection generation and reconcile the last applied command. A new receiver
process establishes a new generation, fences the previous one, and reports actual
state; it must not blindly execute old pending commands.
Supply server time and remaining command lifetime so a receiver can enforce the
deadline with a monotonic timer despite clock skew. R0 must specify how reconnect
affects that lifetime; reconnect never extends it.

Persist registrations, command acceptance/idempotency and terminal results using
existing encrypted storage. Atomically record acceptance before notifying the
receiver. On server restart, invalidate presence and live connection generations;
mark unconfirmed commands outcome-unknown where execution cannot be excluded.
Do not replay them as new commands. Persisted snapshots are historical until a
fresh receiver report. Reconnect establishes a new generation and reconciles
state/last-applied ID; completed results remain queryable across restart.

Define auth refresh on open streams: close by token expiry and reconnect with the
current access token, or explicitly revalidate through a tested equivalent.
Revocation/account-library changes actively close affected streams and reject
reports/acks; periodic liveness work alone is not an adequate revocation mechanism.
Wire both individual device revoke and account-wide revocation. Browser logout
closes its controller feed, not the receiver's independent session. Receiver logout
revokes that receiver. A controller cannot report state or ack for any receiver.

Do not promise instant physical silence on revocation when the device has already
buffered/downloaded audio. Enforce server authorization and specify cooperative
receiver stop behavior on revocation; separately preserve existing media semantics.
Do not log credentials, leased URLs, arbitrary error bodies or private queue dumps.

## Browser integration and handoff semantics

Introduce one output abstraction used by all playback entry points. Keep local
behavior intact when no receiver is selected. Audit transport buttons, keyboard
shortcuts, media-session handlers, track/album/playlist actions, quick actions,
shuffle, enqueue, seek, volume, automatic next and state persistence. Render remote
state without letting dormant local Audio events overwrite it. Keep the local
saved queue separate from the remote snapshot; do not repeatedly write remote
state through `/api/state` or another device's `/queue`.

Transfer local -> remote:

1. Capture current item, queue and actual local position at click time. Preserve
   paused/playing state. Submit load with target revision and a stable retry key.
2. Keep local playback until the receiver confirms the requested playing/paused
   state at the intended source. An “accepted” or “buffering” ack is not completion.
3. Stop local audio, select the remote output and render its reported state. A
   small gap/overlap and position drift during preparation are acceptable in v1.
4. On explicit rejection, keep local output. On missing ack, show uncertain status
   and reconcile by command ID/state; do not assert failure or issue a new load.
   The receiver must abandon work that has not started by its deadline. If playback
   already started, a late report can resolve success; timeout is not a rollback.

Guard transfer completion with the initiating browser operation/version. A late
ack from an abandoned transfer must not stop a newly selected local track or switch
the UI back to an old target. Show the remote's actual state independently and
offer an explicit stop/reconciliation action if that abandoned load took effect.

For remote -> This browser, obtain a fresh resumable snapshot, stop the remote and
wait for confirmation, then start local playback from that snapshot. Handle browser
autoplay rejection with a visible Play action. If stop is unconfirmed/unreachable,
do not automatically start local audio; allow an explicit user choice explaining
the receiver may still be playing. Never silently resume local audio on disconnect.
Reopening/reloading the browser may reattach to the last selected owned receiver
after fresh discovery; it must not automatically start local playback.

No receivers: show a useful empty state explaining that a receiver must sign in
and connect. Do not ship a fake ShopWeazl row. Unsupported controls are disabled
with an explanation. Current remote mode and pending/error state must be visible
in desktop and mobile layouts, with accessible names and keyboard behavior.

Flight Recorder remains local: require explicit return to This browser before
starting it while controlling remote playback. Disable Mood creation/queue-follow
while remote or transferring in v1, and require finishing/cancelling active Mood
before handoff. Existing completed Mood playlists remain ordinary castable library
queues. Do not allow a background Mood callback to mutate the wrong queue.
The receiver alone owns remote listening/scrobble occurrences; observing controllers
do not submit duplicate scrobbles. Preserve existing local behavior.

## Implementation board

### R0 — Freeze contract and capture baseline

- [x] Record current HEAD, working-tree changes and relevant test baseline.
- [x] Write normative `docs/REMOTE_PLAYBACK_V1.md` with exact schemas, errors,
  revision mapping, command ordering, retry/reconnect and handoff sequences.
- [x] Add sanitized fixtures under `fixtures/v1/remote/` and schema/contract tests.
- [x] Record deviations from proposed defaults here before dependent work begins.

Exit: an independent receiver author can implement auth, discovery, queue loading,
event consumption, reporting, deduplication and lease retrieval from the contract.

### R1 — Server control and presence

- [x] Implement capability discovery, receiver enrollment and ownership isolation.
- [x] Implement bounded SSE, snapshots, generations, reports and presence expiry.
- [x] Implement commands, revision checks, durable idempotency/status and acks.
- [x] Implement refresh/revocation/restart cleanup without global-lock stalls.
- [x] Add retention/rate bounds and useful redacted diagnostics.

Exit: two owners and multiple devices cannot cross-control; retries cannot double
advance a track; a stalled receiver cannot stall unrelated library/recorder work.

### R2 — Isolated receiver and protocol acceptance

- [x] Add a reproducible CLI receiver simulator in this repository, using normal
  native auth and public protocol routes, not privileged test-only server routes.
- [x] Support configurable startup delay, failure, dropped ack, duplicate event,
  physical-button change, disconnect and process restart.
- [x] Resolve actual synthetic library/radio leases and consume bounded audio;
  verify finite seek/range behavior separately from simulated playback reports.
- [x] Document running it against `cmd/weazlfixture` with temporary storage.

Exit: commands/status are verified independently of browser mocks. Simulated
playing state is never described as audible playback or physical Pi validation.

### R3 — Browser output selection and controls

- [x] Implement output abstraction, native-envelope API adapter and Play on… UI.
- [x] Wire all entry points, transfer/return flows and remote state rendering.
- [x] Preserve local queue restoration, radio resume, recorder and local Mood.
- [x] Add Playwright coverage using the isolated receiver, including mobile UI.

Exit: browser sends a real queue through the public server API; simulated receiver
requests media; browser becomes a controller only after confirmed transfer.

### R4 — Reviewable release candidate

- [x] Complete the acceptance matrix below and record actual commands/results.
- [x] Update `docs/API_REFERENCE.md`, `docs/SUBWEAZL_API_V1.md`, fixture index,
  `README.md`, `docs/DEPLOYMENT.md` and `docs/VERIFICATION.md` as appropriate.
- [x] Record final extension revision, source commits, limitations and rollback
  compatibility. No physical receiver acceptance claim yet.
- [x] Present implementation and evidence for owner review; await release acceptance.

### R5 — Later deployment/publication, after owner acceptance

- [x] Inspect actual production checkout/image/config; preserve newer work.
- [x] Follow current deployment backup/rollback procedure for the entire volume
  and encryption key. Record image IDs and any migration compatibility limits.
- [x] Deploy the accepted source; verify health, auth, capability discovery,
  existing playback/recorder behavior and proxy SSE delivery with allowed checks.
- [x] Publish the accepted commits to the confirmed GitHub branch and record exact
  source SHA/image digest so published code and production can be reconciled.
- [x] Update verification receipt. Do not mutate real playlists/history as tests.

### R6 — Later ShopWeazl handoff, after deployment and GitHub update

- [ ] Create the requested separate receiver implementation handoff, referencing
  the released contract revision and immutable published commit.
- [ ] Include tested setup/login, configured server origin/identity, fixtures,
  command/report examples, refresh/revocation and retry behavior, audio-owner
  coordination, physical controls, receiver UI and device acceptance checklist.
- [ ] State which server checks passed and which physical Pi/Android checks remain.

Do not fill R5/R6 with hypothetical production outcomes or create an apparently
final receiver handoff before the server contract has actually shipped.

## Required acceptance matrix

| Area | Evidence required |
| --- | --- |
| Identity | Alice/Bob isolation for lists, direct-ID reads, events, commands, reports and acks; same-account wrong-device rejection; library identity change and revoked-device rejection. |
| Auth lifetime | Controller logout leaves receiver independent; receiver revoke terminates stream; bearer expiry/refresh reconnect; browser expiry does not leak events. |
| Transfer | Current track + duplicate-containing queue + position; paused source; empty queue; live station; unavailable track/station; unsupported capability; unsaved station handling. |
| Ordering | Two controllers racing; physical next before delayed remote seek; stale revisions; duplicate command and ack; changed payload under same key; rapid slider input. |
| Failures | Slow prepare, explicit rejection, lost ack after playback starts, command deadline, offline target and late reconnect; no delayed surprise playback or false rollback claims. |
| Restart | Persisted command acceptance/result; crash before/after delivery/ack; new server epoch; new receiver generation; fresh presence required; no automatic replay of uncertain work. |
| SSE | Snapshot/subscribe race; replay cursor; expired cursor; bounded slow reader; auth renewal; revocation; shutdown cleanup; proxy buffering guidance. |
| Browser | Local audio stops only on success; remote controls/physical changes reflected; close/reopen; no local autoplay on disconnect; safe Play here; mobile/keyboard/media-session controls. |
| Queues | Remote enqueue/shuffle preserve current position; repeated IDs retain occurrence identity; no overwrite of local/native saved queues; autonomous next after controller closes. |
| Existing features | Local library/radio, radio probe/metadata behavior, Mood and Flight Recorder unaffected; Weazlpod/native API compatibility and scoped media protections preserved. |
| Resource bounds | Queue/body limits, numeric validation, max receivers/streams, command/report rate limits, retention cleanup and no growing telemetry journal. |

Use synthetic Navidrome/radio, temporary DATA_DIR and isolated test volumes only.
Use injected clocks for deadlines/presence and deterministic fault injection rather
than long sleeps. At completion run the repository's race suite, build and browser
suite (install test dependencies if needed):

```sh
go test -race ./...
make build
tests/run-browser.sh
```

Run focused protocol tests during development, then the required complete checks
on the final implementation. Report skips, missing dependencies and failures
honestly. Add a targeted sustained reconnect/report test for cleanup; this feature
does not require rerunning unrelated multi-hour recorder soaks without cause.

## Decisions, evidence and completion record

Sol maintains this section as implementation proceeds:

- Contract decisions/deviations: extension `2026-11-01.1` is specified in
  `docs/REMOTE_PLAYBACK_V1.md`. SSE sends bounded full snapshots every 250ms instead
  of a replay journal; every reconnect is a resync. This removes cursor gaps and
  retains explicit command IDs/deduplication. Radio reports use zero position and
  duration instead of null, with kind-based seek gating. Ordinary native login
  provides enrollment; short-code pairing remains deferred. Recovery of unresolved
  work uses a fresh receiver generation or a newer authoritative report.
  Source namespaces are derived from authenticated installation/account/library
  and player identity rather than duplicated in each playback descriptor. Public
  commands still cannot grant cross-library media permission.
- Enrollment and command idempotency are separate from the generic API mutex.
  Receiver telemetry remains memory-only. Receiver registration and command results
  persist in existing encrypted records, without a schema-version change.
- Baseline: HEAD `236a7c1`; pre-existing changes were this workbook and its README
  link from planning. No other working-tree changes were present. The first broad
  race validation ran with the new implementation; no pristine baseline run is
  claimed.
- Implementation commits: `9cbe373` (backend/protocol/simulator), `35c797d`
  (browser and acceptance). Verification receipt: `docs/VERIFICATION.md`,
  “Remote playback release candidate — 2026-11-01”.
- Validation: `go test -race ./...` passed (server suite 127.808s),
  `go vet ./...` passed, `make build` passed, and `tests/run-browser.sh` passed
  both existing browser regressions and the new isolated receiver acceptance.
  New receiver tests cover real media leases, repeat occurrences, pause/next,
  enqueue/volume, radio with a held queue, return to local, rejected startup,
  lost acknowledgements, receiver process restart, and mobile output selection.
- Reproducible test entry points: `internal/server/remote*_test.go`,
  `tests/remote.cjs`, `scripts/remote-receiver.py`. Local command logs are
  `/tmp/remote-race-release.log`, `/tmp/remote-vet-release.log`,
  `/tmp/remote-build-release.log`, `/tmp/remote-browser-release.log`.
  Fixture start polling logs transient connection refusals before readiness;
  all acceptance assertions complete successfully.
- Known limitations: no physical Pi, Android, iOS or actual audio-output acceptance.
  The simulator consumes bounded audio bytes and models transport; it is not an
  audible receiver. Full-snapshot SSE uses four snapshots/second; very large queues
  across many simultaneous controllers need deployment-specific throughput review.
  Uncertain operations may require receiver reconnection; no remote phone wakeup,
  short-code enrollment, multiroom or Flight Recorder transfer is implemented.
  Disk-failure/power-loss injection and physical network partition behavior are not
  exhaustively qualified by these local tests; command acceptance/results are
  persisted before notification/confirmation and restart recovery is tested.
- Owner release acceptance: 2026-11-02; user requested production deployment.
  User explicitly waived volume backup because a fresh Proxmox snapshot was taken.
  Retained the previous container image for immediate binary rollback.
- Production: `weazlmusic.teralab.local`, source `6cc9162`; healthy, zero restarts.
  Receipt: `/home/bobp/weazlmusic-backups/20261102-remote-playback`.
  Image and rollback details are recorded in `docs/VERIFICATION.md`.
- GitHub publication: owner authorized 2026-11-02, `bprendie/weazlmusic`, main.
  Includes the remote playback commits, deployment receipt and verified Compose
  build update; see the publication receipt in `docs/VERIFICATION.md`.
- Subsequent ShopWeazl handoff path: pending.
