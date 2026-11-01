# Remote playback extension 2026-11-01.1

Implementation candidate. No production or physical receiver acceptance claimed.
Base `/api/v1/remote`. Existing native authentication/envelopes, browser cookies
and CSRF rules apply. Native receiver enrollment uses ordinary login and refresh.
All objects are account/library scoped. Receiver ID equals its native device ID.
No LAN connections or token-bearing event URLs are used.

## Enrollment and events

`PUT /receiver` body `{name,sourceKinds,commands}` registers the authenticated native
receiver. Name 1..100 bytes; kinds library/radioLive; controls from capability list.
`POST /receiver/connections` body `{}` creates a connection generation, fencing
prior generations. Both require UUID Idempotency-Key and return player snapshots.
Identical retries return their original response; changed bodies fail 409. Keys
from an earlier server epoch fail 409; after server restart use a new key.
Reuse a connection after network/credential refresh, create a new one after process
restart or to reconcile/fence an uncertain command. Native clients should verify
`/info` before reusing credentials at another configured origin.

`GET /receiver/events?connectionId=…` is native bearer SSE. Event `snapshot` contains
`{player,commands,serverTime}`. Each commands entry is `{command,remainingMs}`.
One receiver stream/device. Controller `GET /events` emits `snapshot` containing an
array of owned players. Limit eight controller streams/account. Snapshots repeat
every 250ms, acting as full state resync and heartbeat; no replay journal is needed.
Last-Event-ID never causes command execution; reconnect always gets current state.
IDs contain server epoch and monotonic event sequence. Slow writes have a five
second deadline. Separate command queries allow reconciling dropped notifications.

A player has `id,name,libraryId,sourceKinds,commands,connectionId,serverEpoch,
revision,reportSequence,commandSequence,state,lastReportAt,online`. `revision` is
the controller ETag number. Online requires attached stream and valid report within
30 seconds. Registration/credential lastSeen alone does not indicate availability.

`POST /receiver/reports` body `{connectionId,sequence,state}`. Sequence strictly
increases within a generation; identical latest retry succeeds, conflicting or
older sequence fails 409. Full state each report, every 2s playing / 10s idle.
`POST /receiver/commands/{id}/ack` body `{connectionId,status,state}`, status
applied/rejected. Applied means the requested source/transport actually took effect,
never merely buffering. Identical acks deduplicate; changed results fail. Reports
and acks have their own sequence/ID semantics and need no UUID.

## Controllers and commands

`GET /players` and `GET /players/{id}` accept cookie/bearer auth. Single player
returns ETag `"vN"`. `POST /players/{id}/commands` requires UUID Idempotency-Key and
exact If-Match. Body `{type,playback?,positionMs?,volume?}`. Types load, replaceQueue,
play, pause, stop, next, previous, seek, setVolume. Returns 202 command; poll
`GET /players/{id}/commands/{commandId}` for outcome. 202 means accepted only.

Commands contain `id,playerId,connectionId,receiverRevision,commandSequence,type,
playback,positionMs,volume,status,expiresAt,createdAt`. Status queued, delivered,
applied, rejected or outcome_unknown. Idempotency is scoped to controller identity,
account, library, player and UUID. Same UUID/body/If-Match returns the same command;
changed content returns 409. Lifetime 15s; one unresolved command per receiver.
Missing If-Match 428; stale revision/busy/offline 409; invalid/unsupported 422.
Ownership failure returns 404; native-only receiver operations reject controllers.

Receiver checks generation, local receiverRevision and increasing commandSequence
before execution. Use remainingMs from the first delivery and a monotonic timer;
reconnect must not extend the deadline. Retain applied IDs/results and deduplication
for 24h; repeated delivery re-acks known results, never repeats next/load/seek.
A newer receiver-local revision rejects a delayed command even before its report
has reached the server. Report physical buttons and autonomous track advancement.

## Playback and observed state

Library playback: `{kind:"library",queue:[{entryId,trackId}],currentEntryId,
positionMs,playWhenReady}`. Metadata on entries is optional: title,artist,album,
duration (seconds),coverArt. Metadata is display-only. Radio:
`{kind:"radioLive",stationId,queue:[],positionMs:0,playWhenReady,heldLibrary?}`.
heldLibrary is a library descriptor. No raw URLs or local-cache references.
Duplicate track IDs require distinct occurrence entryIds. Limit 1000 entries,
1MiB request, positions 0..604800000 ms. Radio stations must already be saved.
Queue and permissions remain separate from legacy `/queue` and `/api/state`.

State: `{playback,transport,positionMs,durationMs,volume,receiverRevision,
lastAppliedCommandId,error,radioMetadata?}`. Transport idle/buffering/playing/paused/ended/error;
volume 0..1; revision nonnegative integer; bounded sanitized error (256 bytes).
Optional radioMetadata is `{title,artist}`, at most 512 combined bytes. Radio has no seek; report zero position/duration. Heartbeats/position ticks retain
receiverRevision. Queue/track/transport/volume/seek/error changes increase it.
The server control revision changes only with discrete state, not position ticks.
Acks must carry the applied command ID and a newer receiverRevision.

The receiver obtains fresh trackStream/radioLive/cover leases using its own native
credentials and resolves returned relative URLs against the verified server origin.
It renews expired leases and never forwards credentials to radio origins. Resource
access is checked by existing media routes when fetched, not by eagerly fetching
1000 queue entries at submission. Queue metadata never confers media permission.
Preserve existing private-radio origin/redirect rules and normal codec negotiation.

Receiver advances the loaded queue independently. Next past end stops; previous
at first seeks zero. Radio Next resumes heldLibrary when available. Stop retains
resumable state. replaceQueue preserves current audio/position when the current
occurrence is retained. Receiver alone owns listening/scrobble occurrences;
controllers observing playback must not submit duplicate scrobbles.

## Recovery, storage and limits

An expired unacknowledged command is outcome_unknown, not a proven rollback.
A report with lastAppliedCommandId resolves it applied; a newer receiverRevision
fences it rejected. Otherwise recover by starting a new receiver connection before
accepting further work. Acks/reports from previous generations fail. Never execute
old pending work on a new connection. Server restart clears presence/connections
and marks unconfirmed work outcome_unknown; results remain queryable. Registrations,
commands and idempotency records use existing encrypted SQLite records. Telemetry
is memory-only. Command/result retention 24h; clients must not retry beyond it.
Registration limit 16/account, mutations 20/s/account or receiver; 429 Retry-After:1.

Streams close on native revocation, browser session cancellation, access expiry,
server shutdown or generation replacement. Reconnect with refreshed credentials.
Account/library changes invalidate devices and visibility. Controller logout leaves
receiver independent. Receiver should stop cooperatively on revocation; the server
cannot retract already buffered audio. No background wakeup guarantee on Android.

## Browser behavior and validation

Play on selects an owned available receiver. Keep local playback until confirmed
applied. An unconfirmed transfer displays uncertainty and must reconcile; it cannot
be declared failed merely because a response was lost. Late transfer completion
cannot stop a newer local selection. This browser snapshots remote state, confirms
stop, then resumes locally; unreachable stop requires explicit user choice. No
automatic local playback on disconnect. Return local before choosing another target.
Flight Recorder and active Mood require local output; ordinary saved Mood playlists
are library queues. No multiroom, Flight Recorder transfer, cross-account/server,
short-code pairing, direct Navidrome control or phone wakeup in this extension.

Isolated simulator (consumes media, no audible output):

```sh
go run ./cmd/weazlfixture
python3 scripts/remote-receiver.py --state-file /tmp/weazl-fixture-receiver.json
node tests/remote.cjs
```

Use disposable state files, fixture accounts and loopback servers only. Simulator
flags: --delay seconds, --reject, --drop-ack, --disconnect-after seconds,
--physical-next-after seconds. Process restart reuses credentials but creates a
new connection generation. Production credentials and live ShopWeazl are excluded.
