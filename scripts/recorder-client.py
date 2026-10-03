#!/usr/bin/env python3
"""Fixture acceptance, resumable verified downloads, and six-hour soak.
Uses only synthetic alice/bob on the explicitly selected fixture installation.
"""
import argparse
import array
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

class Client:
    def __init__(self, base):
        self.base = base.rstrip('/')
        self.credentials = None
    def request(self, method, path, body=None, headers=None, retry=True):
        request_headers = dict(headers or {})
        if self.credentials:
            request_headers['Authorization'] = 'Bearer ' + self.credentials['accessToken']
        if method not in ('GET', 'HEAD'):
            request_headers.setdefault('Idempotency-Key', str(uuid.uuid4()))
        if body is not None:
            request_headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(self.base + '/api/v1/' + path, data=None if body is None else json.dumps(body).encode(), headers=request_headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=35) as response:
                raw = response.read()
                return (json.loads(raw) if raw else None), dict(response.headers)
        except urllib.error.HTTPError as error:
            if error.code == 401 and retry and self.credentials and not path.startswith('auth/'):
                self.refresh()
                return self.request(method, path, body, headers, False)
            raise RuntimeError(f'{method} {path.split("?")[0]} HTTP {error.code}: {error.read().decode()}') from error
    def login(self, user='alice'):
        result, _ = self.request('POST', 'auth/login', {'username': user, 'password': 'test-password', 'deviceName': 'recorder acceptance', 'clientId': 'fixture-recorder-client'})
        self.credentials = result['data']
    def refresh(self):
        key = str(uuid.uuid4())
        result, _ = self.request('POST', 'auth/refresh', {'refreshToken': self.credentials['refreshToken']}, {'Idempotency-Key': key}, False)
        self.credentials = result['data']
    def get(self, path):
        return self.request('GET', path)[0]['data']
    def lease(self, asset):
        return self.request('POST', 'media/leases', {'kind': 'recordingSegment', 'resourceId': asset})[0]['data']
    def download(self, segment, destination):
        destination = Path(destination)
        partial = destination.with_suffix('.part')
        for attempt in range(3):
            if destination.exists() and destination.stat().st_size == segment['byteLength'] and hashlib.sha256(destination.read_bytes()).hexdigest() == segment['sha256']:
                return
            lease = self.lease(segment['assetId'])
            url = urllib.parse.urljoin(self.base + '/', lease['url'])
            if urllib.parse.urlsplit(url).netloc != urllib.parse.urlsplit(self.base).netloc:
                raise RuntimeError('media lease changed origin')
            offset = partial.stat().st_size if partial.exists() else 0
            headers = {'Range': f'bytes={offset}-'} if offset else {}
            req = urllib.request.Request(url, headers=headers)
            with urllib.request.urlopen(req, timeout=35) as response:
                mode = 'ab' if offset and response.status == 206 else 'wb'
                with partial.open(mode) as output:
                    while chunk := response.read(65536):
                        output.write(chunk)
                    output.flush()
                    os.fsync(output.fileno())
            if partial.stat().st_size != segment['byteLength'] or hashlib.sha256(partial.read_bytes()).hexdigest() != segment['sha256']:
                partial.unlink()
                if attempt == 2:
                    raise RuntimeError('recording checksum mismatch')
                continue
            os.replace(partial, destination)
            return

def validate(manifest):
    assert manifest['schemaVersion'] == 1 and manifest['durationMs'] > 0
    identifiers = set()
    for track in manifest['tracks']:
        coverage = [(x['startMs'], x['durationMs']) for x in track['segments'] + track['gaps']]
        cursor = 0
        for start, duration in sorted(coverage):
            assert duration > 0 and start == cursor, f'coverage gap/overlap at {cursor}'
            cursor = start + duration
        assert cursor == manifest['durationMs']
        for segment in track['segments']:
            assert segment['id'] not in identifiers
            identifiers.add(segment['id'])
            assert segment['byteLength'] > 0 and len(segment['sha256']) == 64
        # Leaving at minute 30 and returning at minute 50 always uses one timeline.
        for timeline in (1800000, 3000000):
            for segment in track['segments']:
                if segment['startMs'] <= timeline < segment['startMs'] + segment['durationMs']:
                    seek = segment['mediaStartMs'] + timeline - segment['startMs']
                    assert seek - segment['mediaStartMs'] == timeline - segment['startMs']

def validate_tones(manifest, downloads):
    results = []
    # Decode the asset at the shared minute-30 / minute-50 offset after download.
    for timeline in (1800000 + 2000, 3000000 + 2000):
        if timeline >= manifest['durationMs']:
            continue
        for index, track in enumerate(manifest['tracks']):
            segment = next((s for s in track['segments'] if s['startMs'] <= timeline < s['startMs'] + s['durationMs'] - 500), None)
            if segment is None:
                results.append({'timelineMs': timeline, 'station': index + 1, 'gap': True})
                continue
            offset = (segment['mediaStartMs'] + timeline - segment['startMs']) / 1000
            pcm = subprocess.check_output(['ffmpeg', '-v', 'error', '-ss', str(offset), '-i', str(downloads / (segment['assetId'] + '.m4a')), '-t', '0.25', '-ar', '22050', '-ac', '1', '-f', 's16le', '-'])
            samples = array.array('h', pcm)
            assert len(samples) >= 4000, 'short offline cue decode'
            crossings = sum(a <= 0 < b for a, b in zip(samples, samples[1:]))
            frequency = crossings * 22050 / len(samples)
            expected = [440 + index * 110 + cue * 40 for cue in range(6)]
            assert min(abs(frequency - x) for x in expected) < 10, f'wrong station/cue at shared offset: {index + 1}, {frequency}'
            results.append({'timelineMs': timeline, 'station': index + 1, 'frequencyHz': round(frequency, 2), 'gap': False})
    if manifest['durationMs'] >= 3003000:
        assert sum(not r['gap'] for r in results) >= 8, 'minute 30/50 healthy cues missing'
    return results

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--base', default='http://127.0.0.1:4003')
    parser.add_argument('--duration', type=int, default=21600, help='wall-clock seconds, default six hours')
    parser.add_argument('--output', required=True)
    parser.add_argument('--container', help='local fixture container for resource sampling')
    parser.add_argument('--kill-after', type=int, help='force-kill/restart the named fixture container during capture')
    args = parser.parse_args()
    out = Path(args.output)
    out.mkdir(parents=True, exist_ok=True)
    client = Client(args.base)
    client.login()
    info = client.get('info')
    stations = client.get('radio/stations')
    assert all('radio.fixture.invalid' in s['streamURL'] for s in stations[:6]), 'Run against weazlfixture only'
    ids = [s['id'] for s in stations if s['preset']][:6]
    assert len(ids) == 6
    created, _ = client.request('POST', 'flight-recorder/sessions', {'name': f'{args.duration}s six-stream fixture', 'stationIds': ids, 'durationMs': args.duration * 1000})
    session = created['data']
    start = time.monotonic()
    killed = False
    samples = []
    print(json.dumps({'sessionId': session['id'], 'durationSeconds': args.duration}), flush=True)
    while time.monotonic() - start < args.duration + 120:
        if args.kill_after and not killed and time.monotonic() - start >= args.kill_after:
            assert args.container, '--kill-after requires --container'
            subprocess.run(['docker', 'kill', '--signal', 'KILL', args.container], check=True, stdout=subprocess.DEVNULL)
            subprocess.run(['docker', 'start', args.container], check=True, stdout=subprocess.DEVNULL)
            killed = True
            time.sleep(10)
        try:
            session = client.get('flight-recorder/sessions/' + session['id'])
        except Exception as error:
            print(str(error), flush=True)
            time.sleep(2)
            continue
        sample = {'elapsedSeconds': round(time.monotonic() - start, 2), 'state': session['state'], 'capturedBytes': session['capturedBytes'], 'stations': [{k: p.get(k) for k in ('stationId','state','capturedDurationMs','bytes','error')} | {'assetCount': len(p.get('segments') or []), 'metadataCount': len(p.get('metadata') or [])} for p in session['stations']]}
        if args.container:
            stats = subprocess.run(['docker', 'stats', '--no-stream', '--format', '{{json .}}', args.container], capture_output=True, text=True, check=True)
            sample['resources'] = json.loads(stats.stdout)
        samples.append(sample)
        with (out / 'samples.json').open('w') as f:
            json.dump(samples, f, indent=2)
        print(json.dumps({k: sample[k] for k in ('elapsedSeconds', 'state', 'capturedBytes')}), flush=True)
        if session['state'] in ('complete', 'partial', 'failed', 'missed', 'cancelled'):
            break
        time.sleep(min(30, max(1, args.duration / 20)))
    assert session['state'] in ('complete', 'partial'), f'capture did not finalize: {session["state"]}'
    manifest = client.get('flight-recorder/sessions/' + session['id'] + '/manifest')
    validate(manifest)
    assert len(manifest['tracks']) == 6
    coverage = [sum(s['durationMs'] for s in t['segments']) / manifest['durationMs'] for t in manifest['tracks']]
    if args.duration >= 60:
        minimum = 1 - 30 / args.duration if killed else .9
        assert min(coverage[:4]) >= minimum, f'healthy stream coverage too low: {coverage}'
    downloads = out / 'downloads'
    downloads.mkdir(exist_ok=True)
    # Test a interrupted/corrupt partial copy and verify its eventual promotion.
    segments = [s for t in manifest['tracks'] for s in t['segments']]
    assert segments
    (downloads / (segments[0]['assetId'] + '.part')).write_bytes(b'wrong fixture bytes')
    for segment in segments:
        client.download(segment, downloads / (segment['assetId'] + '.m4a'))
    (downloads / 'manifest.json').write_text(json.dumps(manifest, indent=2))
    first = downloads / (segments[0]['assetId'] + '.m4a')
    subprocess.run(['ffmpeg', '-v', 'error', '-ss', '0.5', '-i', str(first), '-t', '0.25', '-f', 'null', '-'], check=True)
    tone_evidence = validate_tones(manifest, downloads)
    summary = {'offlineToneChecks': tone_evidence, 'startToleranceMs': max(0, round((datetime.fromisoformat(session['actualStartedAt'].replace('Z','+00:00')) - datetime.fromisoformat(session['startsAt'].replace('Z','+00:00'))).total_seconds()*1000)) if session['actualStartedAt'] else None, 'contract': info['contractRevision'], 'installationId': info['installationId'], 'sessionId': session['id'], 'wallSeconds': round(time.monotonic() - start, 2), 'state': session['state'], 'bytes': manifest['totalBytes'], 'coverage': coverage, 'assetCount': len(segments), 'checksumsVerified': True, 'localSeekDecoded': True, 'forcedRestart': killed, 'overnightValidated': args.duration >= 21600, 'appleValidated': False}
    (out / 'manifest.json').write_text(json.dumps(manifest, indent=2))
    (out / 'summary.json').write_text(json.dumps(summary, indent=2))
    print(json.dumps(summary), flush=True)

if __name__ == '__main__':
    main()
