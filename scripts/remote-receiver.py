#!/usr/bin/env python3
"""Protocol receiver simulator. Consumes media bytes; does not render audible audio.
Use only disposable fixture accounts. Password via WEAZL_FIXTURE_PASSWORD.
"""
import argparse
import copy
import json
import os
import threading
import time
import urllib.request
import urllib.error
import uuid
from urllib.parse import urlsplit

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--server', default='http://127.0.0.1:4003')
parser.add_argument('--user', default='alice')
parser.add_argument('--name', default='Simulated ShopWeazl')
parser.add_argument('--state-file', required=True)
parser.add_argument('--delay', type=float, default=0)
parser.add_argument('--reject', action='store_true')
parser.add_argument('--drop-ack', action='store_true')
parser.add_argument('--disconnect-after', type=float, default=0)
parser.add_argument('--physical-next-after', type=float, default=0)
args = parser.parse_args()
origin = urlsplit(args.server)
if origin.scheme not in ('http','https') or origin.hostname not in ('127.0.0.1','localhost') or origin.username or origin.password or origin.path not in ('','/'):
    parser.error('Simulator is restricted to loopback fixture servers.')
lock = threading.RLock()
try:
    with open(args.state_file) as f:
        saved = json.load(f)
except FileNotFoundError:
    saved = {}
creds = saved.get('credentials', {})
state = saved.get('state', dict(playback=None, transport='idle', positionMs=0,
                               durationMs=0, volume=.7, receiverRevision=0,
                               lastAppliedCommandId='', error=''))
connection = ''
sequence = 0
applied_sequence = 0
results = {}

def persist():
    temporary = args.state_file + '.tmp'
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as f:
        json.dump(dict(credentials=creds, state=state), f)
    os.replace(temporary, args.state_file)

def api(path, body=None, method=None, key=None):
    headers = {'Content-Type': 'application/json'}
    if creds.get('accessToken'):
        headers['Authorization'] = 'Bearer ' + creds['accessToken']
    if body is not None:
        headers['Idempotency-Key'] = key or str(uuid.uuid4())
    req = urllib.request.Request(args.server + '/api/v1/' + path,
                                 data=None if body is None else json.dumps(body).encode(),
                                 headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.load(response)['data']

def report():
    global sequence
    sequence += 1
    api('remote/receiver/reports', dict(connectionId=connection, sequence=sequence,
                                       state=state))

def consume():
    playback = state['playback']
    if not playback:
        return
    if playback['kind'] == 'library':
        entry = next(e for e in playback['queue'] if e['entryId'] == playback['currentEntryId'])
        resource, kind = entry['trackId'], 'trackStream'
        state['durationMs'] = int(entry.get('duration', 180) * 1000)
    else:
        resource, kind = playback['stationId'], 'radioLive'
        state['durationMs'] = state['positionMs'] = 0
    lease = api('media/leases', dict(kind=kind, resourceId=resource))
    from urllib.parse import urljoin, urlsplit
    media = urljoin(args.server, lease['url'])
    if urlsplit(media).netloc != urlsplit(args.server).netloc:
        raise ValueError('Cross-origin lease rejected')
    req = urllib.request.Request(media, headers={'Range': 'bytes=0-4095'} if kind == 'trackStream' else {})
    with urllib.request.urlopen(req, timeout=10) as audio:
        if not audio.read(4096):
            raise ValueError('Empty audio')

def advance(delta):
    p = state['playback']
    if not p:
        return
    if p['kind'] == 'radioLive':
        if p.get('heldLibrary'):
            state['playback'] = p['heldLibrary']
            state['positionMs'] = p['heldLibrary']['positionMs']
            consume()
        return
    index = next(i for i, e in enumerate(p['queue']) if e['entryId'] == p['currentEntryId']) + delta
    if index >= len(p['queue']):
        state['transport'] = 'ended'
    else:
        p['currentEntryId'] = p['queue'][max(0, index)]['entryId']
        p['positionMs'] = state['positionMs'] = 0
        consume()

if not creds:
    creds = api('auth/login', dict(username=args.user,
                password=os.environ.get('WEAZL_FIXTURE_PASSWORD', 'test-password'),
                deviceName=args.name, clientId='remote-fixture'))
    persist()
else:
    # New process uses existing device identity, with refresh and a new generation.
    creds = api('auth/refresh', dict(refreshToken=creds['refreshToken']))
    persist()
api('remote/receiver', dict(name=args.name, sourceKinds=['library', 'radioLive'],
    commands=['load', 'replaceQueue', 'play', 'pause', 'stop', 'next', 'previous', 'seek', 'setVolume']), 'PUT')

started = time.monotonic()
def telemetry():
    physical_done = False
    while True:
        time.sleep(2)
        with lock:
            if not connection:
                continue
            try:
                if state['transport'] == 'playing' and state.get('playback', {}).get('kind') == 'library':
                    state['positionMs'] += 2000
                    if state['durationMs'] and state['positionMs'] >= state['durationMs']:
                        advance(1)
                        state['receiverRevision'] += 1
                if args.physical_next_after and not physical_done and time.monotonic()-started >= args.physical_next_after:
                    advance(1)
                    state['receiverRevision'] += 1
                    physical_done = True
                report()
                persist()
            except (urllib.error.URLError, ValueError):
                pass
threading.Thread(target=telemetry, daemon=True).start()
print('Simulator ready; consumes bytes, no audible output.', flush=True)
while True:
    try:
        with lock:
            if not connection:
                player = api('remote/receiver/connections', {})
                connection = player['connectionId']
                sequence = applied_sequence = 0
                state['receiverRevision'] = max(state['receiverRevision'], player['state']['receiverRevision']) + 1
                report()
        req = urllib.request.Request(args.server + '/api/v1/remote/receiver/events?connectionId=' + connection,
                                      headers={'Authorization': 'Bearer ' + creds['accessToken']})
        with urllib.request.urlopen(req, timeout=20) as events:
            for line in events:
                if args.disconnect_after and time.monotonic()-started >= args.disconnect_after:
                    raise SystemExit(0)
                if not line.startswith(b'data: '):
                    continue
                snapshot = json.loads(line[6:])
                for delivery in snapshot['commands']:
                    c = delivery['command']
                    deadline = time.monotonic() + delivery['remainingMs']/1000
                    with lock:
                        if c['id'] in results:
                            if not args.drop_ack:
                                api('remote/receiver/commands/'+c['id']+'/ack', results[c['id']])
                            continue
                        valid = c['connectionId'] == connection and c['receiverRevision'] == state['receiverRevision'] and c['commandSequence'] > applied_sequence
                    time.sleep(args.delay)
                    with lock:
                        valid = valid and c['receiverRevision'] == state['receiverRevision'] and time.monotonic() < deadline
                        before = copy.deepcopy(state)
                        status = 'rejected'
                        if valid and not args.reject:
                            try:
                                t = c['type']
                                if t in ('load', 'replaceQueue'):
                                    retained = t == 'replaceQueue' and state.get('playback') and state['playback'].get('kind') == c['playback']['kind'] and state['playback'].get('currentEntryId') == c['playback'].get('currentEntryId') and state['playback'].get('stationId') == c['playback'].get('stationId')
                                    state['playback'] = c['playback']
                                    state['positionMs'] = c['playback']['positionMs']
                                    if not retained:
                                        consume()
                                    state['transport'] = 'playing' if c['playback']['playWhenReady'] else 'paused'
                                elif t in ('play','pause','stop'):
                                    state['transport'] = dict(play='playing',pause='paused',stop='idle')[t]
                                elif t in ('next','previous'):
                                    advance(1 if t == 'next' else -1)
                                elif t == 'seek':
                                    state['positionMs'] = c['positionMs']
                                elif t == 'setVolume':
                                    state['volume'] = c['volume']
                                state['receiverRevision'] += 1
                                state['lastAppliedCommandId'] = c['id']
                                applied_sequence = c['commandSequence']
                                status = 'applied'
                            except (ValueError, urllib.error.URLError):
                                state = before
                        ack = dict(connectionId=connection,status=status,state=copy.deepcopy(state))
                        results[c['id']] = ack
                        persist()
                        if not args.drop_ack:
                            api('remote/receiver/commands/'+c['id']+'/ack', ack)
    except urllib.error.HTTPError as e:
        if e.code == 401:
            with lock:
                creds = api('auth/refresh', dict(refreshToken=creds['refreshToken']))
                persist()
        elif e.code in (403, 409):
            connection = ''
        else:
            print('Receiver HTTP status:', e.code, flush=True)
        time.sleep(1)
    except (urllib.error.URLError, TimeoutError):
        time.sleep(1)
