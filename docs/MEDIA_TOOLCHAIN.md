# Recorder media toolchain

The user's `~/ipod_script/ipod.py` profile1 decodes s16 WAV at44100Hz/stereo and
uses fdkaac profile2 at160k. The recorder matches that encode path, then carries
AAC over ADTS pipes to FFmpeg's copy-only M4A segment muxer. Published segments
are faststart, seekable, independently validated with ffprobe for AAC LC/44100/2,
exact duration, bytes and SHA-256. This is lossy transcoding; it can save space
against higher bitrate sources and enlarge lower bitrate ones.

Go validates public URL/DNS/redirects and reads ICY on the one source connection.
FFmpeg receives a pipe and a pipe/file protocol allowlist; it does not receive
upstream credentials or fetch radio URLs. Independent workers use one decode,
one fdkaac and one copy mux process per station; there are at most six stations.
All staging is on the durable data filesystem, not the8MiB container/tmp.

| Component | Tested/pinned choice |
| --- | --- |
| Host Go | go1.27.0-X:nodwarf5 linux/amd64 |
| Container Go | go1.26.8 linux/amd64; pinned1.26-alpine digest in Dockerfile |
| Container OS | Alpine3.23; pinned digest in Dockerfile |
| FFmpeg/ffprobe | Alpine ffmpeg8.0.1-r1 (8.0.1) |
| fdkaac | Alpine1.0.6-r0 with fdk-aac2.0.2-r4 |
| Host media tools | FFmpeg n9.0.1, fdkaac1.0.9 |
| SQLite | modernc.org/sqlite1.39.1; go.sum pins dependencies; no CGO |

The Alpine FFmpeg build enables GPL and version3 components; retain its applicable
license/source obligations when distributing the image. fdkaac is zlib licensed;
FDK AAC has its own upstream license. See primary
[FFmpeg licensing](https://ffmpeg.org/legal.html),
[fdkaac source/license](https://github.com/nu774/fdkaac),
[FDK AAC license](https://github.com/mstorsjo/fdk-aac/blob/master/NOTICE), and
[Alpine FFmpeg package](https://pkgs.alpinelinux.org/package/v3.23/community/x86_64/ffmpeg).
The image installs the complete Alpine media packages rather than an unmaintained
custom demuxer. Package/base pins must be deliberately updated and revalidated.

Local production image reports61,591,397 bytes via docker image inspect; fixture
image adds the synthetic runner. Runtime package installation reports about135MiB
unpacked. In the180-second six-stream container run, capture samples ranged
17.76–78.41% Docker CPU (100%=one core); the last active sample used222.8MiB RAM.
These short-run samples are not six-hour resource bounds. Soak sampling excludes
credentials and records compact station progress to avoid duplicating every
segment in every sample. Use production30-second segments for the six-hour run
(`FIXTURE_SEGMENT_SECONDS=30` in the fixture container); short fixture checks use5.

Standard-player evidence includes actual Chromium audio readyState/currentTime
advancement, FFmpeg decode/seeks and AAC/VBR-MP3 inputs. Apple compatibility is
pending hardware tests. Staging validation rejects incomplete files; detected
restart/disconnect gaps preserve the shared timeline. Sub-15-second in-attempt
stalls and independent broadcaster delivery offsets remain timing limitations.
