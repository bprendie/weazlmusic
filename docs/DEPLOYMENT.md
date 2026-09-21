# Deployment

The container listens on `0.0.0.0:4000`. Put the reverse proxy in front of that
port and preserve the incoming `Host` header. TLS termination, certificates,
DNS, and public access policy belong to the proxy.

For a proxy that supports a simple upstream, the shape is:

```text
music.example.test  ->  http://127.0.0.1:4000
```

Keep response buffering disabled for `/api/mood` and `/api/radio/events`; both
routes stream incremental events. The app sends `X-Accel-Buffering: no` for
these routes, but the proxy must honor it. Allow long-lived responses for radio
audio and Mood generation. No WebSocket upgrade is required.

Set `COOKIE_SECURE=true` when users reach the app only through HTTPS. Leave it
false for direct local HTTP smoke tests. Do not set a Navidrome URL in the
environment: the administrator saves it through **Installation settings**.

The named `weazltunes-data` volume contains the encryption key and encrypted
records. Update with `git pull && docker compose up -d --build`; do not use
`docker compose down -v` during an upgrade. Back up the whole volume, including
the key. The app is one process and one data volume; use a single replica.

The health endpoint is `GET /healthz`. A healthy container exposes port 4000 and
returns `{"status":"ok"}`. Sessions are intentionally in memory and expire on
restart; accounts, installation settings, queues, and radio stations persist.
