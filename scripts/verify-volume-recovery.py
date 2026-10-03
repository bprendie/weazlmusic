#!/usr/bin/env python3
"""Stop/back up/restore ONLY a caller-selected synthetic fixture container."""
import importlib.util
import json
from pathlib import Path
import subprocess
import time
import uuid

spec = importlib.util.spec_from_file_location('recorder_client', Path(__file__).with_name('recorder-client.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
c = m.Client('http://127.0.0.1:4005')
c.login()
assert all('radio.fixture.invalid' in x['streamURL'] for x in c.get('radio/stations')[:6])
container = 'weazl-workbook-restart'
info = c.get('info')
sessions = c.get('flight-recorder/sessions')
assert sessions and all(s['state'] in ('complete', 'partial', 'failed', 'missed', 'cancelled') for s in sessions)
inspect = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
mount = next(x for x in inspect['Mounts'] if x['Destination'] == '/data')
assert mount['Type'] == 'volume' and mount['Name'].startswith('weazl-workbook-')
key = str(uuid.uuid4())
old = c.credentials['refreshToken']
rotated, _ = c.request('POST', 'auth/refresh', {'refreshToken': old}, {'Idempotency-Key': key})
c.credentials = rotated['data']
out = Path('test-results/volume-recovery').resolve()
out.mkdir(parents=True, exist_ok=True)
restored = 'weazl-workbook-restored'
volume = 'weazl-workbook-restored-data'
def run(*args):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL)
run('docker', 'stop', container)
try:
    run('docker', 'run', '--rm', '--user', '0', '--entrypoint', 'tar', '-v', mount['Name']+':/source:ro', '-v', str(out)+':/backup', 'alpine:3.23', '-czf', '/backup/data.tar.gz', '-C', '/source', '.')
    run('docker', 'volume', 'create', volume)
    run('docker', 'run', '--rm', '--user', '0', '--entrypoint', 'tar', '-v', volume+':/restore', '-v', str(out)+':/backup:ro', 'alpine:3.23', '-xzf', '/backup/data.tar.gz', '-C', '/restore')
    run('docker', 'run', '-d', '--name', restored, '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true', '--init', '--tmpfs', '/tmp:size=8m,mode=1777', '-e', 'LISTEN_ADDR=0.0.0.0:4000', '-p', '127.0.0.1:4006:4000', '-v', volume+':/data', 'weazltunes-fixture:workbook')
    c.base = 'http://127.0.0.1:4006'
    for attempt in range(40):
        try:
            assert c.get('info')['installationId'] == info['installationId']
            replay, _ = c.request('POST', 'auth/refresh', {'refreshToken': old}, {'Idempotency-Key': key}, False)
            assert replay == rotated, 'lost refresh result not restored'
            break
        except (OSError, RuntimeError):
            time.sleep(.5)
    else:
        raise RuntimeError('restored fixture did not start')
    got = c.get('flight-recorder/sessions')
    assert [(s['id'], s['state'], s['capturedBytes']) for s in got] == [(s['id'], s['state'], s['capturedBytes']) for s in sessions]
    manifest = c.get('flight-recorder/sessions/'+sessions[0]['id']+'/manifest')
    m.validate(manifest)
    segment = next(s for t in manifest['tracks'] for s in t['segments'])
    c.download(segment, out / (segment['assetId']+'.m4a'))
    result = {'installationPreserved': True, 'nativeRefreshReplayPreserved': True, 'ownedSessionsPreserved': True, 'assetChecksumVerified': True}
    (out/'summary.json').write_text(json.dumps(result, indent=2)+'\n')
    print(json.dumps(result))
finally:
    subprocess.run(['docker', 'rm', '-f', restored], stdout=subprocess.DEVNULL)
    run('docker', 'start', container)
