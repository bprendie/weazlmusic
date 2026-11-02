#!/usr/bin/env bash
# Exercise the tracked Compose build, not a separately maintained docker build.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ -n "$(git status --porcelain)" ]]; then
  echo 'Run Compose verification from a clean committed checkout.' >&2
  exit 1
fi
export SOURCE_REVISION
SOURCE_REVISION=$(git rev-parse HEAD)
verify_work=$(mktemp -d)
verify_project="weazl-compose-check-$$"
verify_image="weazltunes-compose-check:$$"
verify_config="$verify_work/compose.json"
cleanup() {
  if [[ -f "$verify_config" ]]; then
    docker compose -p "$verify_project" -f "$verify_config" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  docker image rm "$verify_image" >/dev/null 2>&1 || true
  rm -rf "$verify_work"
}
trap cleanup EXIT
# Compose resolves context/env from this exact checkout; isolate only image name,
# ports, project and data volume. Preserve the published build and pull policy.
docker compose config --format json > "$verify_work/resolved.json"
python3 - "$verify_work/resolved.json" "$verify_config" "$verify_image" <<'PY'
import json,sys
source,target,image=sys.argv[1:]
config=json.load(open(source))
service=config['services']['weazltunes']
assert service['pull_policy']=='build', 'Compose can reuse a stale image'
assert service['build']['target']=='production'
service['image']=image
service['ports']=[dict(target=4000,published='0',host_ip='127.0.0.1',protocol='tcp')]
# Do not use any production volume, even if the source config changes later.
service['volumes']=[dict(type='volume',source='verify-data',target='/data')]
service['environment']['COOKIE_SECURE']='false'
config['volumes']={'verify-data':{}}
config['networks']={'default':{}}
config.pop('name',None)
with open(target,'w') as f: json.dump(config,f)
PY
# A deliberately unstartable stale image proves up must actually rebuild.
docker build -t "$verify_image" - > "$verify_work/seed.log" 2>&1 <<'DOCKER'
FROM scratch
LABEL org.opencontainers.image.revision="stale-test-image"
CMD ["/stale-image-must-not-run"]
DOCKER
verify_stale=$(docker image inspect "$verify_image" --format '{{.Id}}')
docker compose -p "$verify_project" -f "$verify_config" up -d
verify_container=$(docker compose -p "$verify_project" -f "$verify_config" ps -q weazltunes)
verify_actual=$(docker inspect "$verify_container" --format '{{.Image}}')
test "$verify_actual" != "$verify_stale"
verify_label=$(docker inspect "$verify_container" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')
test "$verify_label" = "$SOURCE_REVISION"
verify_port=$(docker compose -p "$verify_project" -f "$verify_config" port weazltunes 4000)
python3 - "http://$verify_port" "$SOURCE_REVISION" <<'PY'
import hashlib,json,pathlib,sys,time,urllib.request,urllib.error
base,revision=sys.argv[1:]
for attempt in range(60):
    try:
        with urllib.request.urlopen(base+'/healthz',timeout=2) as r:
            assert json.load(r)['status']=='ok'
        break
    except (OSError,AssertionError):
        time.sleep(1)
else: raise SystemExit('Compose image never became ready')
for name in ['remote.js','player.js','app.js','index.html']:
    with urllib.request.urlopen(base+'/'+name,timeout=5) as r: served=r.read()
    assert hashlib.sha256(served).digest()==hashlib.sha256(pathlib.Path('web',name).read_bytes()).digest(),name+' differs from checkout'
request=urllib.request.Request(base+'/api/v1/auth/login',data=json.dumps(dict(username='weazladmin',password='admin',deviceName='compose-verification',clientId='compose-verification')).encode(),headers={'Content-Type':'application/json'})
with urllib.request.urlopen(request,timeout=10) as r: token=json.load(r)['data']['accessToken']
request=urllib.request.Request(base+'/api/v1/capabilities',headers={'Authorization':'Bearer '+token})
with urllib.request.urlopen(request,timeout=5) as r: caps=json.load(r)['data']
assert caps['remotePlayback']['enabled'] is True
assert caps['remotePlayback']['revision']=='2026-11-01.1'
request=urllib.request.Request(base+'/api/v1/remote/players',headers={'Authorization':'Bearer '+token})
with urllib.request.urlopen(request,timeout=5) as r: assert json.load(r)['data']==[]
print('PASS: Compose replaced stale image; OCI revision '+revision+'; served assets match checkout; authenticated remote playback capability and API verified.')
PY
