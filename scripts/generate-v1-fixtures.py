#!/usr/bin/env python3
"""Export public DTOs only from weazlfixture. Never export usable credentials."""
import argparse
import importlib.util
import json
from datetime import datetime, timedelta, timezone
from pathlib import Path

spec = importlib.util.spec_from_file_location('recorder_client', Path(__file__).with_name('recorder-client.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
parser = argparse.ArgumentParser()
parser.add_argument('--base', default='http://127.0.0.1:4005')
parser.add_argument('--output', default='fixtures/v1')
args = parser.parse_args()
out = Path(args.output)
out.mkdir(parents=True, exist_ok=True)
c = module.Client(args.base)
c.login()
stations = c.get('radio/stations')
assert all('radio.fixture.invalid' in x['streamURL'] for x in stations[:6]), 'Fixture server required'
def save(name, value):
    (out / (name + '.json')).write_text(json.dumps(value, indent=2) + '\n')
def export(name, path):
    value, _ = c.request('GET', path)
    save(name, value)
    return value
credentials = dict(c.credentials)
credentials['accessToken'] = 'fixture-only-access'
credentials['refreshToken'] = 'fixture-only-refresh'
save('auth', {'data': credentials})
for name, path in [('info','info'), ('capabilities','capabilities'), ('identity','me'), ('devices','auth/devices'), ('tracks','library/tracks?limit=2'), ('albums','library/albums?limit=2'), ('artists','library/artists?limit=2'), ('album','library/albums/0'), ('artist','library/artists/0'), ('track','library/tracks/0-0'), ('stations','radio/stations'), ('storage','flight-recorder/storage'), ('queue','queue'), ('curator','preferences/curator')]:
    export(name, path)
playlist, _ = c.request('POST','library/playlists', {'name':'Contract duplicate fixture','trackIds':['0-0','1-0','0-0']})
save('playlist', playlist)
export('playlists', 'library/playlists')
for kind in ('track','album','artist'):
    result, _ = c.request('PUT', 'library/favorites/'+kind+'/'+('0-0' if kind=='track' else '0'), {'enabled':True})
    save('favorite-'+kind, result)
export('favorites','library/favorites')
scrobble, _ = c.request('POST','library/scrobbles', {'occurrenceId':'fixture-occurrence','trackId':'0-0','playedAt':'2026-10-03T20:00:00Z'})
save('scrobble',scrobble)
lease, _ = c.request('POST','media/leases', {'kind':'trackOriginal','resourceId':'0-0'})
lease['data']['url'] = '/api/v1/media/assets/fixture-lease?lease=fixture-only'
save('lease',lease)
start = (datetime.now(timezone.utc)+timedelta(days=1)).replace(hour=2,minute=0,second=0,microsecond=0)
schedule, _ = c.request('POST','flight-recorder/schedules', {'name':'Contract overnight fixture','stationIds':[x['id'] for x in stations[:6]], 'startsAt':start.isoformat(), 'endsAt':(start+timedelta(hours=6)).isoformat(), 'timeZone':'America/New_York','recurrence':{'kind':'once'}})
save('schedule',schedule)
export('schedules','flight-recorder/schedules')
export('sessions','flight-recorder/sessions')
save('mood-job',{'data':{'jobId':'fixture-mood','state':'running'}})
save('mood-events',{'events':[{'sequence':1,'type':'progress','playlist':{'id':'fixture-playlist','name':'Mood','songCount':0},'count':0,'target':20},{'sequence':2,'type':'selection','playlist':{'id':'fixture-playlist','name':'Mood','songCount':1},'track':c.get('library/tracks/0-0'),'count':1,'target':20},{'sequence':3,'type':'failed','count':1,'target':20,'message':'Fixture interruption; confirmed tracks remain saved.'}]})
save('curator-models',{'data':{'models':['fixture-model']}})
for code, status in [('unauthorized',401),('session_revoked',401),('forbidden',403),('not_found',404),('validation_failed',422),('version_conflict',409),('snapshot_expired',409),('capacity_conflict',409),('storage_full',507),('upstream_unavailable',502),('upstream_rejected',502),('outcome_unknown',409),('unsupported',422),('asset_expired',410),('rate_limited',429)]:
    save('error-'+code,{'error':{'code':code,'message':'Sanitized '+code+' fixture.','retryable':status>=500 or status==429,'details':{}}})
print('Exported synthetic DTOs; credential and lease fields are inert placeholders.')
