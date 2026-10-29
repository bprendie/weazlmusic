# Library search, quick actions and browser downloads

Implemented locally on 2026-10-29. These changes affect the browser UI and its
session-authenticated compatibility routes. Native API revision `2026-10-03.2`,
Weazlpod negotiation, stream defaults and media representations are unchanged.
Deployed to weazlmusic.teralab.local on 2026-10-29 after explicit authorization; see VERIFICATION.md for the receipt.

Search requests both albums (up to 40) and tracks (up to 100). Album results appear
above tracks, including when no tracks match. Track models retain `albumId`.
“Play album” fetches that ID's complete album in upstream album order and starts
at its first track, replacing the upcoming queue. It keeps the search results
visible. It does not play only the matching songs or infer an album from its name.
For older saved queue/current items without `albumId`, `getSong` resolves the ID;
unavailable albums produce an error instead of playing unrelated search results.
Search also works while viewing Flight Recorder.

## UI pass

| Surface | Quick actions |
| --- | --- |
| Tracks: search, favorites, album, playlist, home | Play now, play next, queue, play/open album, add to playlist, favorite/unfavorite, download original. Owned playlist rows also offer remove from playlist. |
| Album cards: home, collection, search | Open/play album, play next, queue album, add to playlist, download album ZIP. |
| Playlist cards | Open/play playlist, play next, queue playlist, add to playlist, download ZIP. Owner-only rename/delete reuse the existing dialogs. |
| Queue: sidebar and full view | Play now/next, remove this occurrence, album, playlist, favorite and original download actions for library items. Moving an occurrence does not duplicate it. |
| Now Playing | Play/pause, queue, album, playlist, favorite and original download for library playback. The menu is hidden for recorder playback. |
| Radio: presets, directories, saved stations | Tune in, save/remove preset, save station, remove a saved station. Existing preset limits apply. No finite download is offered for a live stream. |
| Recorder schedules | Edit and cancel future recordings through the existing controls. |
| Recorder sessions | Existing Listen/delete or Stop controls, according to state. Delete keeps its confirmation and version/ownership checks. |

Right-click or use the visible “…” button. Keyboard users can open the menu with
Shift+F10 or the context-menu key; arrows/Home/End move focus, Enter/Space select,
Escape dismisses and restores focus, and Tab returns to the surrounding UI.
Menus stay within the viewport and dismiss on outside clicks, user scrolling,
resize, navigation and sign-out. Browser text/input/link context menus are kept.
Sign-out also aborts pending quick-action requests. Existing primary buttons and
keyboard shortcuts remain available. At narrow widths, track queue/playlist
shortcuts move into the menu to leave space for titles and a touch target.
Navigation, search inputs, transport, volume and settings retain their direct
controls rather than receiving redundant contextual menus.

## Downloads

All links use the WeazlMusic origin and signed-in browser session. Upstream
credentials never enter download URLs. “Download original” calls the upstream
`download` endpoint, preserving original file bytes rather than upstream player
transcoding defaults. Filenames use artist/title and the original suffix or MIME
type; unsafe characters and paths are removed. Individual downloads forward
Range/If-Range and upstream lengths, ranges and validators when supplied.

Albums and playlists download as one streamed ZIP of original files, with
numbered entries preserving upstream order and duplicate playlist occurrences.
Audio files are stored without recompression. No audio is staged to disk or
buffered in memory; only bounded collection/ZIP directory metadata is retained.
There are at most two concurrent collection ZIPs globally and 1,000 tracks per
collection. ZIPs have no precomputed size/validator and cannot resume; retry from
the beginning. Track downloads can use upstream byte-range support.

Unavailable/empty/oversize collections fail before attachment headers. The first
original response is checked before ZIP output; later source/transport failures
abort the response without a ZIP central directory, so a partial subset cannot
be mistaken for a complete album. Disconnect, session expiry and logout cancel
upstream I/O and release the ZIP slot. Browser download status reports completion
or failure; the UI toast only acknowledges the request. These routes provide
library files, not live radio files or recorder exports. Recorder offline copies
continue through the existing Subweazl workflow.

## Verification

`tests/quick-actions.cjs`, invoked by `tests/run-browser.sh`, covers mixed and
album-only search, complete album order, original and ZIP browser downloads,
queue occurrence moves/removal, keyboard/focus/dismissal/viewport behavior,
mobile menus, playlist ownership actions, radio download gating and recorder
state/confirmation delegation with isolated fixtures. The existing smoke flow
also checks audio playback, playlist writes, account isolation and mobile layout.

`internal/server/download_test.go` covers original bytes despite a differing
stream default, Range/If-Range, HEAD, ZIP entry bytes/order/duplicates, safe names,
authentication/upstream denial, empty/oversize/concurrent limits, source failures,
transport abort and disconnect/logout cancellation/slot recovery. See
[VERIFICATION.md](VERIFICATION.md) for actual run evidence. These checks do not
claim physical touch-device or every browser's large-download acceptance.
