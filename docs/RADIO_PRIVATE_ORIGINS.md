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
