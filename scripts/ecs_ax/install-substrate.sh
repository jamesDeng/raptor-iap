#!/usr/bin/env bash
# Dedicated raptor-ecs-ax node only. Source archive must match third_party/ax/pins.json.
set -euo pipefail
[[ "$(hostname)" == raptor-ecs-ax ]] || exit 1
install -d -m 700 /opt/raptor/substrate
cd /opt/raptor/substrate
tar --no-same-owner --warning=no-unknown-keyword -xzf /var/lib/raptor-ecs-ax/downloads/ecs-ax-substrate.tar.gz
git init -q
git add .
git -c user.name=Raptor -c user.email=poc@localhost commit -qm 'Pinned upstream ac41c06ee8929ee05159d679febab970a9e5cf4c'
docker rm -f raptor-registry >/dev/null
docker run -d --restart=always --name raptor-registry -p 127.0.0.1:5001:5000 -p 10.70.1.180:5001:5000 -v /var/lib/raptor-ecs-ax/registry:/var/lib/registry registry@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373
cat > /etc/rancher/k3s/registries.yaml <<'CONFIG'
mirrors:
  "localhost:5001":
    endpoint:
      - "http://127.0.0.1:5001"
CONFIG
systemctl restart k3s
# Keep generated credentials on the dedicated node and in Kubernetes Secret only.
python3 - <<'PY'
import pathlib,re,json,secrets,subprocess
root=pathlib.Path('/opt/raptor/substrate')
for rel in ['manifests/ate-install/kind/kustomization.yaml','manifests/ate-install/kind/atelet/kustomization.yaml','manifests/ate-install/kind/rustfs.yaml']:
 p=root/rel;s=p.read_text()
 s=s.replace('kind-registry:5000','10.70.1.180:5001')
 s=re.sub(r'(        +)- name: (AWS_ACCESS_KEY_ID|AWS_SECRET_ACCESS_KEY|RUSTFS_ACCESS_KEY|RUSTFS_SECRET_KEY)\n\1  value: "?rustfsadmin"?',lambda m:m[1]+'- name: '+m[2]+'\n'+m[1]+'  valueFrom:\n'+m[1]+'    secretKeyRef:\n'+m[1]+'      name: snapshot-s3\n'+m[1]+'      key: '+('access-key' if m[2] in ['AWS_ACCESS_KEY_ID','RUSTFS_ACCESS_KEY'] else 'secret-key'),s)
 p.write_text(s)
p=root/'manifests/ate-install/kind/postgres/kustomization.yaml';p.write_text(p.read_text().replace('value: 1Gi','value: 8Gi'))
ns={'apiVersion':'v1','kind':'Namespace','metadata':{'name':'ate-system'}}
subprocess.run(['kubectl','apply','-f','-'],input=json.dumps(ns),text=True,check=True)
existing = subprocess.run(['kubectl','get','secret','snapshot-s3','-n','ate-system','-o','name'],capture_output=True,text=True)
if existing.returncode == 0:
 print('Keeping existing snapshot credentials')
 raise SystemExit(0)
secret={'apiVersion':'v1','kind':'Secret','metadata':{'name':'snapshot-s3','namespace':'ate-system'},'stringData':{'access-key':secrets.token_hex(16),'secret-key':secrets.token_hex(32)}}
subprocess.run(['kubectl','apply','-f','-'],input=json.dumps(secret),text=True,check=True,stdout=subprocess.DEVNULL)
PY
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
export KUBECTL_CONTEXT=default NO_DEV_ENV=1 KO_DOCKER_REPO=localhost:5001
export GOFLAGS=-p=2 GOMAXPROCS=4
# Use the reviewed small-node overlay; no Kind cluster or Docker-in-Docker node is created.
go run ./cmd/ate-setup --kind --context default --rollout-timeout 5m --credential-provider '{"enabled":false}' deploy ate-system
