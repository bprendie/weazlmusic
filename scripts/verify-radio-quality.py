#!/usr/bin/env python3
"""Authenticated server acceptance, without firmware or production library edits.

Reads a private JSON file: {"server":{"public_url":...,"username":...,"password":...}}.
Reuses the requested saved station or temporarily creates/removes one. Creates and
logs out its own device session. Never prints credentials or leased URLs.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def verify_frames(data, bitrate):
    rates = (0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0)
    offset = frames = 0
    while offset + 4 <= len(data):
        h = data[offset:offset + 4]
        if not (h[0] == 255 and h[1] & 254 == 250 and h[2] & 12 == 0
                and rates[h[2] >> 4] == bitrate and h[3] >> 6 == 1):
            raise RuntimeError("Incorrect MP3 frame format")
        size = 144000 * bitrate // 44100 + ((h[2] >> 1) & 1)
        if offset + size > len(data):
            break
        offset += size
        frames += 1
    if frames < 100:
        raise RuntimeError("Too few verified audio frames")
    return data[:offset], frames


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--credentials", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)
    p.add_argument("--station-url", default="https://radio.prendie.io/radio.mp3")
    p.add_argument("--seconds", type=int, default=60)
    args = p.parse_args()
    if not 5 <= args.seconds <= 120:
        p.error("seconds must be between 5 and 120")
    os.umask(0o077)
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    config = json.loads(args.credentials.read_text())["server"]
    base = config["public_url"].rstrip("/")
    if urllib.parse.urlsplit(base).scheme != "https":
        raise RuntimeError("Acceptance requires HTTPS with certificate verification")
    opener = urllib.request.build_opener(NoRedirect())
    token = None

    def api(method, path, body=None, headers=None, expected=200):
        h = {"Accept": "application/json"}
        if token:
            h["Authorization"] = "Bearer " + token
        if method not in ("GET", "HEAD"):
            h["Idempotency-Key"] = str(uuid.uuid4())
        h.update(headers or {})
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            h["Content-Type"] = "application/json"
        req = urllib.request.Request(base + "/api/v1/" + path, data=data, method=method, headers=h)
        try:
            res = opener.open(req, timeout=30)
        except urllib.error.HTTPError as e:
            res = e
        with res:
            raw = res.read(2_000_001)
            if res.code != expected or len(raw) > 2_000_000:
                raise RuntimeError(f"API {method} {path.split('?')[0]} returned HTTP {res.code}, expected {expected}")
            return (json.loads(raw).get("data") if raw else None), dict(res.headers)

    login, _ = api("POST", "auth/login", {
        "username": config["username"], "password": config["password"],
        "deviceName": "Radio quality server acceptance", "clientId": "weazlpod-radio-quality-check",
    })
    token = login["accessToken"]
    created = None
    results = []
    try:
        caps, _ = api("GET", "capabilities")
        available = caps["weazlpod"]["radioProfiles"]
        if {x["profile"] for x in available} != {"mp3-64", "mp3-96", "mp3-128"}:
            raise RuntimeError("Server does not advertise all radio profiles")
        stations, _ = api("GET", "radio/stations")
        station = next((s for s in stations if s["streamURL"] == args.station_url), None)
        if station is None:
            station, _ = api("POST", "radio/stations", {
                "name": "Temporary radio quality acceptance", "streamURL": args.station_url, "preset": False,
            }, expected=201)
            created = station
        body = {"kind": "radioLive", "resourceId": station["id"]}
        identity = {"X-Weazl-Client": "weazlpod"}
        legacy, _ = api("POST", "media/leases", body, identity)
        if legacy["profile"] != "mp3-128":
            raise RuntimeError("Legacy default changed")
        api("POST", "media/leases", dict(body, quality="mp3-128"),
            dict(identity, **{"X-Weazl-Radio-Quality": "low"}), expected=422)
        for quality, bitrate in (("low", 64), ("med", 96), ("high", 128)):
            profile = f"mp3-{bitrate}"
            lease, _ = api("POST", "media/leases", body,
                           dict(identity, **{"X-Weazl-Radio-Quality": quality}))
            expected = {"profile": profile, "codec": "mp3", "bitRateKbps": bitrate,
                        "sampleRateHz": 44100, "channels": 2, "bitRateMode": "cbr", "channelMode": "joint_stereo"}
            if any(lease.get(k) != v for k, v in expected.items()):
                raise RuntimeError("Lease does not match requested encoding")
            url = urllib.parse.urljoin(base + "/", lease["url"])
            if urllib.parse.urlsplit(url)[:2] != urllib.parse.urlsplit(base)[:2]:
                raise RuntimeError("Media lease changed origin")
            start = time.monotonic()
            captured = bytearray()
            first = None
            last = start
            largest_gap = 0
            # Token and selection headers are deliberately absent on media GET.
            with opener.open(url, timeout=20) as res:
                if res.headers.get("Content-Type") != "audio/mpeg" or res.headers.get("X-Weazl-Radio-Profile") != profile:
                    raise RuntimeError("Unexpected stream response headers")
                if any(res.headers.get(k) for k in ("Icy-Metaint", "Content-Length", "Content-Range", "ETag", "Accept-Ranges")):
                    raise RuntimeError("False finite/metadata claim on live stream")
                while first is None or time.monotonic() - first < args.seconds:
                    block = res.read1(4096)
                    now = time.monotonic()
                    if not block:
                        raise RuntimeError("Premature radio EOF")
                    if first is None:
                        first = now
                    else:
                        largest_gap = max(largest_gap, now - last)
                    last = now
                    captured.extend(block)
                    if len(captured) > 16_000_000:
                        raise RuntimeError("Capture exceeded bound")
            complete, frames = verify_frames(captured, bitrate)
            sample = args.output / (profile + ".mp3")
            sample.write_bytes(complete)
            probe = subprocess.run(["ffprobe", "-v", "error", "-show_entries",
                                    "stream=codec_name,sample_rate,channels,bit_rate", "-of", "json", str(sample)],
                                   capture_output=True, check=True)
            stream = json.loads(probe.stdout)["streams"][0]
            if stream != {"codec_name": "mp3", "sample_rate": "44100", "channels": 2, "bit_rate": str(bitrate * 1000)}:
                raise RuntimeError("ffprobe format mismatch")
            subprocess.run(["ffmpeg", "-v", "error", "-xerror", "-i", str(sample), "-f", "null", "-"],
                           capture_output=True, check=True)
            # Reopening this lease demonstrates released playback/worker ownership.
            reconnected = False
            for attempt in range(8):
                time.sleep(0.5)
                try:
                    with opener.open(url, timeout=20) as res:
                        reconnected = res.code == 200 and bool(res.read1(1024))
                    break
                except urllib.error.HTTPError as e:
                    code = e.code
                    e.close()
                    if code not in (409, 429):
                        raise RuntimeError(f"Reconnect returned HTTP {code}") from None
            if not reconnected:
                raise RuntimeError("Reconnect failed to acquire a released worker")
            row = dict(expected, bytes=len(complete), frames=frames,
                       captureSeconds=round(last - first, 3), audioSeconds=round(frames * 1152 / 44100, 3),
                       firstByteSeconds=round(first - start, 3), largestReadGapSeconds=round(largest_gap, 3),
                       fullDecode=True, reconnect=True)
            results.append(row)
            (args.output / "results.json").write_text(json.dumps(results, indent=2) + "\n")
            print(json.dumps(row), flush=True)
            time.sleep(1)
    finally:
        try:
            if created:
                api("DELETE", "radio/stations/" + created["id"],
                    headers={"If-Match": '"v' + str(created["version"]) + '"'}, expected=204)
        finally:
            api("POST", "auth/logout", expected=204)
    print("PASS: all profiles, legacy default, selector conflict, frame format, full decode and reconnect; test session logged out")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        # HTTP/transport exceptions can include credential-bearing lease URLs.
        print("FAIL: " + (str(exc) if isinstance(exc, RuntimeError) else type(exc).__name__), file=sys.stderr)
        sys.exit(1)
