# Radio presets with private DNS — 2026-10-09

`https://radio.prendie.io/radio.mp3` returned valid HTTP 200 audio/mpeg and MP3
44.1 kHz stereo, but workstation and production container DNS resolved its host
to `10.0.0.3`. The radio transport's public-only destination policy rejected the
connection before requesting audio, producing the browser's generic playback
failure. This was a destination-policy issue rather than an audio codec failure.

## Configuration

Set this in the operator's deployment `.env` and recreate the service:

```dotenv
RADIO_PRIVATE_ORIGINS=https://radio.prendie.io
```

The default remains empty/public-only. Multiple origins are comma-separated.
Use scheme, hostname and optional port only; credentials, stream paths, queries,
fragments and wildcards are rejected at startup. An omitted port means 443 for
HTTPS or 80 for HTTP. Permissions cover the origin's paths and all app users;
only operators can configure them. TLS certificate verification remains enabled.

Separate transports scope the exception to the exact origin. Every newly dialed
DNS answer must be public or, for an explicitly allowed origin, RFC1918/IPv6 ULA
private space. Loopback, link-local, cloud metadata and other reserved addresses
remain blocked. Redirects and playlist targets each select their own policy;
permission does not follow a redirect to another host, scheme or port. The same
resolver serves web/native listening and recording.

## Evidence

- `go test -race ./...` and `go vet ./...` passed.
- Regression tests cover origin validation/normalization, IPv4/IPv6 private and
  forbidden addresses, redirected/playlist destinations, and transport reuse.
- An opt-in live resolver check received 32 KiB from the reported URL.
- `tests/radio-live.cjs` exercises authenticated browser playback against an
  isolated local instance; it requires `WEAZL_TEST_RADIO_URL` and does not use
  production accounts or saved presets. Chromium successfully decoded the live
  station through the app relay, advancing past three seconds without errors.

Commands for explicit live checks:

```sh
WEAZL_TEST_RADIO_ORIGIN=https://radio.prendie.io WEAZL_TEST_RADIO_URL=https://radio.prendie.io/radio.mp3 go test ./internal/server -run TestRadioPrivateOriginLive -count=1 -v
# Start an isolated app on 127.0.0.1:4012 with the origin configured first:
WEAZL_TEST_RADIO_URL=https://radio.prendie.io/radio.mp3 node tests/radio-live.cjs
```

Other stations can still fail because they are offline, have unsupported formats,
or resolve privately without an operator exception. This change does not claim
that every saved preset has been checked.

## Production receipt

Deployed source `9dab51d` on 2026-10-09 to `weazlmusic.teralab.local` via the
trusted jumpbox route. Initially deferred because five stations were recording;
the user explicitly approved deploying immediately with a recording gap.
Production `.env` now sets `RADIO_PRIVATE_ORIGINS=https://radio.prendie.io`;
the running container's environment was checked after recreation.

Runtime image:
`sha256:6b20408a71788169ec08ea4b1c52715b985d9d22ecd1c6afd4fdfeb05348df95`.
Health/root HTTP checks passed; container healthy with zero restarts, anonymous
native identity returned 401, and installation info/key digest stayed unchanged.
All five stations wrote fresh checkpoints at 15:05:55–15:06:03 UTC after restart;
the active recording retained its original 17:24:04 UTC end time.

Full-volume backup and rollback receipts:
`/home/bobp/weazlmusic-backups/20261009T150427Z` (directory0700, archive0600).
Archive SHA-256:
`f45a981d73e9e3c98919bb3a1a1b0b1dda6a45e9809a72ed6cd4d4e82373a8e5`.
Rollback tag: `weazltunes-web:rollback-20261009T150427Z`; previous deployment
`.env`, source/image receipts and compose rollback override are retained there.
The real browser playback check used an isolated local app with the same policy;
production verification did not sign in as a user or mutate saved presets.
