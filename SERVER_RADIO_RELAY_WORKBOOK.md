# Server workbook: intermittent Subweazl live radio failure

Created October 10, 2026. **Server repair in progress. The owner authorized implementation, deployment, commit and push on October 10, 2026.**

## Assignment and success condition

Fix the WeazlMusic radio relay so an Apple player’s short format probe cannot
claim the actual live stream’s playback slot. Preserve duplicate-stream protection,
metadata, authentication and existing recorder behavior. Deployment was authorized in the October 10 follow-up request.

Success means the same authorized media lease accepts an overlapping
`GET Range: bytes=0-1` probe and ordinary audio GET, both HTTP 200 with audio;
a second simultaneous **actual audio** GET still receives 409. Audio can reopen
after disconnect while its metadata listener remains connected. Finally, verify
Subweazl radio on the real phone. Do not declare success from a healthy website,
working direct station URL, one lucky playback attempt or a build alone.

## Read this first: what was actually observed

The earlier phone connection issue was resolved by signing in again using app
**1.0 (19)**. The owner confirmed that recovery worked. Offline mode was off.
This radio issue is a separate follow-up; do not delete the account or downloads,
change the server URL, bypass authentication or blame the Offline switch.

The user subsequently reported: “my internet radio say the audio stream could
not be played.” Radio Prendie is the previously reported preset used in the
following diagnostics; the owner has not separately supplied the error-log code
from their failed playback attempt.

| Check on October 10 | Actual result | What it establishes |
| --- | --- | --- |
| Direct `https://radio.prendie.io/radio.mp3`, bounded 12-second Mac probe | HTTP 200, audio/mpeg, 250,600 bytes; ffprobe: MP3, 44.1 kHz, stereo | Origin supplies valid audio from the Mac. Timeout was the intentional live-stream cutoff. |
| Physical iPhone, signed-in real account, real Apple player through WeazlMusic | First start passed; separate test passed three fresh starts and pause/resume on each | Relay playback sometimes works. Player reported Speaker route and rate 1; no human audibility confirmation was collected. |
| Physical iPhone, one fresh radioLive lease, hold probe open, then ordinary GET to the same leased URL | **Probe HTTP 200; actual audio HTTP 409** | Reproduced the relay’s overlapping-request defect against production. No secrets/leased URLs logged. |

The last check failed its assertion that audio should return 200. It is the
reliable reproduction, despite successful ordinary starts. It is consistent with
the intermittent Apple-player error; do not claim every reported failure has
been traced to this code without the corresponding failed-player diagnostics.

No production server change was made during this investigation. No shipping app
code was changed for radio. The temporary Debug test host was replaced by normal
signed Release 1.0 (19); app inventory confirmed it. App Store submission remains
1.0 (18), independently of this server repair.

## Source and evidence locations

On the Mac:

- App repository: `/Users/bobp/XCODE/subweazl_app`.
- Backend checkout with both existing production fixes and the radio repair:
  `/Users/bobp/XCODE/web_weazltunes_release`, branch `subweazl-release`, HEAD
  `3158d3e` (merge of published main and the radio probe repair).
- Repair commit: `c436aecc33b1ee11164d4b8377ded046ec6a41a1`,
  “Allow native radio probes without claiming live playback.” This is local;
  do not assume it is fetchable from GitHub. Earlier original-checkout equivalent
  was `8127550`.
- Published `origin/main` freshly fetched October 10: `a6d7bbf`,
  “Release WeazlTunes 1.0.0”; it lacks this repair. Production source may have
  uncommitted or later changes: inspect it instead of assuming its commit.
- Relevant files: `internal/server/radio.go`, `radio_events.go`,
  `radio_player_test.go`; native lease dispatch is in `v1_media.go`.
- App opt-in device tests: `SubweazlTests/AppSmokeTests.swift`,
  `InstalledPhoneRadioAcceptanceTests/testSavedPrendieRadioPlayback` and
  `testRelayAllowsProbeAlongsideAudio`.
- Ignored/private Mac evidence: `build/acceptance/radio-device-diagnosis.xcresult`,
  `radio-device-retry.xcresult`, `radio-relay-probe.xcresult`, and corresponding
  `.log` files. The overlap test failed in 42.547 seconds including library restore;
  its redacted line is `Radio relay probe status: 200 audio status: 409`.
- Run log: `docs/XCODE_RUN_LOG.md` in the app repository. Backend prior evidence:
  `SOL_WORKBOOK.md` and `docs/VERIFICATION.md`.

This workbook embeds the complete three-file source/test patch below so the
server agent does not need access to this Mac or its local commit. Do not send
private xcresults/logs, copied library databases or account credentials to Git.

## R0 — Inspect the deployed state before changing anything

- [x] Read the backend’s repository instructions, current workbooks,
  `docs/DEPLOYMENT.md`, `docs/RADIO_PRIVATE_ORIGINS.md` and recorder chunk docs.
- [x] Record current source commit, working-tree modifications, running image and
  Compose configuration; preserve unrelated/newer changes.
- [x] Check the actual deployed `radioStream` and `radioFeed` implementation.
  If the repair is already present, investigate why the running service still
  produced 409 instead of reapplying it blindly (old image, wrong instance,
  deployment not restarted, or a different rejection path).
- [x] Preserve the already-deployed private-origin/DNS authorization repair
  (`9dab51d` in published history) and the owner’s 10-minute, 128-kbps AAC recorder
  chunks. Do not deploy an old checkout that reverses either change.

Native flow: authenticated `radioLive` lease → leased media route → `radioStream`.
The old relay calls `feed.begin()` for every request with the same playback ID.
The probe can hold this flag when the real GET arrives, causing 409. Additionally,
without `feed.end()`, an SSE listener retaining the feed can prevent later audio
reopening even after the audio request ends.

## R1 — Apply the narrow repair

- [x] Apply the embedded patch to the current clean main checkout, preserving the
  existing private-origin and recorder changes. The owner-provided workbook was
  the only untracked file.
- [x] Treat only GET with trimmed `Range: bytes=0-1` as the sniff probe.
- [x] Validate playback IDs for probes too; retain all upstream safety checks,
  media-lease ownership, device/account revocation and expiry validation.
- [x] Do not let the probe acquire/begin/publish/end the live metadata feed.
- [x] Bound the probe’s upstream read to 16 KiB. Do not advertise a finite
  two-byte live asset or static size; remove Content-Length, Content-Range and
  Accept-Ranges from the forwarded probe headers.
- [x] Keep the real audio request as the exclusive feed owner. Defer a
  mutex-protected `feed.end()` so completion/cancellation releases ownership
  even if an SSE listener keeps the feed object alive.
- [x] Preserve ICY stripping, metadata events, redirects and cancellation. Keep
  a duplicate actual audio stream rejected with 409.

Do not solve this by disabling duplicate-owner protection globally, allowing
arbitrary private origins, sending server credentials to the station, bypassing
the relay in the app, or changing the API contract to an invented fallback.
No lease schema change is needed. The patch excludes historical documentation
edits from the original commit; update the current contract notes/workbook to
reflect actual behavior after validation.

## R2 — Validate against isolated fixtures

Run from the backend checkout, never against the production data volume:

```sh
go test -race ./internal/server -run '^TestRadioProbeOverlapsAudioAndReopensWithMetadataListener$' -count=1
go test -race ./...
```

- [x] Probe overlaps audio: both return 200; actual audio bytes arrive.
- [x] A second genuine audio owner returns 409.
- [x] Probe terminates after the bounded read and has no false static length.
- [x] Closing audio releases ownership while a metadata listener retains feed.
- [x] Reopened audio returns 200; cleanup/cancellation and race checks pass.
- [x] Preserve auth/revocation, invalid-ID, SSRF/private-origin authorization,
  redirects, ICY metadata and recording regressions in the existing suite.

Prior evidence for the prepared repair (historical, not a new deployment):
`go test -race ./...` passed, and the isolated real-Apple-player test passed
leased music, unknown-length MP3, ICY, redirect, pause/resume, metadata and held
library restoration. Re-run against the server agent’s final source.

If a compatible Mac is available, the app fixture runner can exercise real
AVPlayer against a source-only, isolated backend snapshot:

```sh
WEAZLTUNES_SOURCE=/absolute/path/to/fixed/backend python3 scripts/run_weazltunes_fixture.py --native --destination 'platform=iOS Simulator,id=SIMULATOR_ID'
```

Run that command from the app repository. It does not certify production or phone
behavior and must not be repointed at the real server/account.

## Server-agent progress on October 10

The Linux production route `ssh -o BatchMode=yes -o HostKeyAlias=jumpbox.prendie.io bobp@jumpbox.teralab.local` followed by nested SSH to `bobp@weazlmusic.teralab.local` passed host-key checks. Production was clean at `a6d7bbf`, with image `sha256:6b20408a71788169ec08ea4b1c52715b985d9d22ecd1c6afd4fdfeb05348df95` healthy with zero restarts. No recording or finalization job was active on the read-only check. The different Mac failure described below is historical and did not apply to this host.

The embedded patch was applied to current main. In addition to its direct relay test, `TestNativeRadioLeaseProbeAndAudio` exercised the actual native station, media lease and media asset routes against an isolated test server. Both focused tests passed with the race detector. `go test -race ./...` and `go vet ./...` passed. A probe and audio GET returned 200/200, an overlapping second audio GET and `Range: bytes=0-2` returned 409, a malformed playback ID returned 400, the probe stopped at 16 KiB without static range headers, and audio reopened after disconnect with the feed retained.

## R3 — Deploy (owner authorized October 10)

- [x] Establish the correct trusted access route. The documented nested route
  is `bobp@jumpbox.prendie.io` → `bobp@weazlmusic.teralab.local`, checkout
  `/home/bobp/weazlmusic`. Those are historical instructions, not verified access.
- [x] This Mac’s attempt via `jumpbox.teralab.local` using
  `HostKeyAlias=jumpbox.prendie.io` failed: no trusted ED25519 host key for the
  alias. Do not disable host-key verification; use the owner’s trusted server
  session or establish the correct verified key/access configuration.
- [x] Inspect active Flight Recorder captures. Coordinate a restart outside
  active recordings; do not silently interrupt or stop them.
- [ ] Follow current `docs/DEPLOYMENT.md` backup/rollback procedure. Preserve the
  actual persistent volume, database, encryption key, presets and recordings.
  Record rollback image/source and backup evidence without logging secrets.
- [ ] Build/deploy the reviewed fixed source using the existing production
  process. Do not run `down -v`, recreate storage, regenerate keys or change ports.
- [ ] Verify running image/source and service health. Keep the existing native
  public contract compatible; `/api/v1/info` should still respond successfully.

A healthy landing page or `/api/v1/info` response is not radio acceptance.

## R4 — Re-run the failing production check and phone playback

Use normal authorized sign-in; acquire a fresh native radioLive lease for the
existing station. Keep all tokens/lease URLs private. Do not create fixture
accounts, mutate presets or start recordings in production for this test.

- [ ] Hold an authenticated `Range: bytes=0-1` probe open, then start the ordinary
  audio GET using the same lease URL. Required result: **200 / 200**, audio bytes.
- [ ] Bound request lifetimes and cancel both connections afterward.
- [ ] Repeat real phone starts and pause/resume, including fresh launch and retry
  after interruption. Check failure diagnostics if any start fails.
- [ ] Owner confirms audible Radio Prendie playback. Then verify Wi-Fi/cellular,
  background playback, metadata and return to held library as separate results.
- [ ] Record deployed commit/image, timestamps, exact tests and remaining gaps
  in backend workbook/verification docs; send evidence back to the app agent.

The existing opt-in physical test `testRelayAllowsProbeAlongsideAudio` is the
regression that currently fails against production. Run it only intentionally
on the signed-in phone. After native testing, restore the normal signed Release
app, preserving its container. Do not replace the App Store submission merely
because of this server-only repair.

## Completion report expected from the server agent

Report: applied commit/diff; preservation of private-origin and recorder changes;
fixture/race results; deployed image/commit and rollback point; overlap status
before 200/409 and after 200/200; owner’s audible phone result; any untested cases.
If deployment is still deferred, say “source validated; not deployed” and leave
R3/R4 open. Do not mark this issue fixed in production from source alone.

## Appendix — portable source/test patch

This diff is against backend `a6d7bbf`. Review against newer source before applying.
Save the block as `radio-relay-probe-fix.patch` outside the source tree, then use
`git apply --check /path/to/radio-relay-probe-fix.patch` followed by `git apply`
in the isolated backend worktree. Resolve changed context deliberately.

```diff
diff --git a/internal/server/radio.go b/internal/server/radio.go
index 09fb0b7..fb879ae 100644
--- a/internal/server/radio.go
+++ b/internal/server/radio.go
@@ -92,11 +92,15 @@ func (s *Server) radioStream(w http.ResponseWriter, r *http.Request, se *session
 		return
 	}
 	publish := func(radioMetadata) {}
-	if id := r.URL.Query().Get("playback"); id != "" {
-		if !playbackID.MatchString(id) {
-			fail(w, 400, "Invalid playback ID")
-			return
-		}
+	// AVPlayer probes a live URL with bytes=0-1, then opens its audio
+	// connection without Range. The probe must not claim the playback ID:
+	// cancellation can overlap the real GET while the relay unwinds.
+	probe := r.Method == http.MethodGet && strings.TrimSpace(r.Header.Get("Range")) == "bytes=0-1"
+	if id := r.URL.Query().Get("playback"); id != "" && !playbackID.MatchString(id) {
+		fail(w, 400, "Invalid playback ID")
+		return
+	}
+	if id := r.URL.Query().Get("playback"); id != "" && !probe {
 		feed, release, err := se.radio.acquire(id)
 		if err != nil {
 			fail(w, 429, err.Error())
@@ -107,6 +111,7 @@ func (s *Server) radioStream(w http.ResponseWriter, r *http.Request, se *session
 			fail(w, 409, "This radio playback is already active")
 			return
 		}
+		defer feed.end()
 		publish = feed.publish
 		defer func() { feed.publish(radioMetadata{Ended: true}) }()
 	}
@@ -120,6 +125,17 @@ func (s *Server) radioStream(w http.ResponseWriter, r *http.Request, se *session
 		return
 	}
 	defer res.Body.Close()
+	if probe {
+		// Live audio has no static size or seek range. Bound abandoned sniff
+		// connections without claiming a two-byte file or retaining a feed.
+		res.Body = struct {
+			io.Reader
+			io.Closer
+		}{io.LimitReader(res.Body, 16*1024), res.Body}
+		res.Header.Del("Content-Length")
+		res.Header.Del("Content-Range")
+		res.Header.Del("Accept-Ranges")
+	}
 	w.Header().Set("X-Accel-Buffering", "no")
 	intervalHeader := res.Header.Get("Icy-Metaint")
 	if intervalHeader == "" {
diff --git a/internal/server/radio_events.go b/internal/server/radio_events.go
index 212938a..c83b354 100644
--- a/internal/server/radio_events.go
+++ b/internal/server/radio_events.go
@@ -58,6 +58,11 @@ func (f *radioFeed) begin() bool {
 	f.active = true
 	return true
 }
+func (f *radioFeed) end() {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	f.active = false
+}
 func (f *radioFeed) publish(meta radioMetadata) {
 	f.mu.Lock()
 	defer f.mu.Unlock()
diff --git a/internal/server/radio_player_test.go b/internal/server/radio_player_test.go
new file mode 100644
index 0000000..b6bbec1
--- /dev/null
+++ b/internal/server/radio_player_test.go
@@ -0,0 +1,96 @@
+package server
+
+import (
+	"bytes"
+	"context"
+	"io"
+	"net/http"
+	"net/http/httptest"
+	"net/url"
+	"sync/atomic"
+	"testing"
+	"time"
+)
+
+func TestRadioProbeOverlapsAudioAndReopensWithMetadataListener(t *testing.T) {
+	var calls atomic.Int32
+	releaseProbe := make(chan struct{})
+	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		first := calls.Add(1) == 1
+		w.Header().Set("Content-Type", "audio/mpeg")
+		w.(http.Flusher).Flush()
+		if first {
+			select {
+			case <-releaseProbe:
+			case <-r.Context().Done():
+				return
+			}
+		}
+		_, _ = w.Write(bytes.Repeat([]byte("A"), 32*1024))
+		w.(http.Flusher).Flush()
+		<-r.Context().Done()
+	}))
+	defer upstream.Close()
+	ctx, cancel := context.WithCancel(context.Background())
+	defer cancel()
+	hub := newRadioHub()
+	se := &session{Expires: time.Now().Add(time.Minute), ctx: ctx, radio: hub}
+	s := &Server{radio: upstream.Client()}
+	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.radioStream(w, r, se) }))
+	defer app.Close()
+	id := "0123456789abcdef0123456789abcdef"
+	// An SSE listener keeps this feed alive across audio disconnects.
+	_, release, err := hub.acquire(id)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer release()
+	client := &http.Client{Timeout: 5 * time.Second}
+	get := func(probe bool) *http.Response {
+		t.Helper()
+		req, _ := http.NewRequest("GET", app.URL+"/?"+url.Values{"url": {upstream.URL}, "playback": {id}}.Encode(), nil)
+		if probe {
+			req.Header.Set("Range", "bytes=0-1")
+		}
+		res, err := client.Do(req)
+		if err != nil {
+			t.Fatal(err)
+		}
+		return res
+	}
+	probe := get(true)
+	defer probe.Body.Close()
+	audio := get(false)
+	defer audio.Body.Close()
+	if probe.StatusCode != 200 || audio.StatusCode != 200 {
+		t.Fatal("probe claimed playback", probe.StatusCode, audio.StatusCode)
+	}
+	buf := make([]byte, 8)
+	if _, err := io.ReadFull(audio.Body, buf); err != nil || string(buf) != "AAAAAAAA" {
+		t.Fatal("audio relay failed", err)
+	}
+	duplicate := get(false)
+	duplicate.Body.Close()
+	if duplicate.StatusCode != 409 {
+		t.Fatal("two actual audio owners accepted", duplicate.StatusCode)
+	}
+	close(releaseProbe)
+	bounded, err := io.ReadAll(probe.Body)
+	if err != nil || len(bounded) != 16*1024 || probe.ContentLength != -1 {
+		t.Fatal("probe must be bounded without a static file size", len(bounded), probe.ContentLength, err)
+	}
+	audio.Body.Close()
+	deadline := time.Now().Add(2 * time.Second)
+	for time.Now().Before(deadline) {
+		reopened := get(false)
+		reopened.Body.Close()
+		if reopened.StatusCode == 200 {
+			return
+		}
+		if reopened.StatusCode != 409 {
+			t.Fatal("unexpected reopen failure", reopened.StatusCode)
+		}
+		time.Sleep(10 * time.Millisecond)
+	}
+	t.Fatal("metadata listener permanently retained an active audio owner")
+}
```
