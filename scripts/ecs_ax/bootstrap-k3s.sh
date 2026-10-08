#!/usr/bin/env bash
# Run only on the dedicated Terraform-owned raptor-ecs-ax POC node.
set -euo pipefail
[[ "$(hostname)" == "raptor-ecs-ax" ]] || { echo 'Wrong host'; exit 1; }
version='v1.37.0+k3s1'
install -d -m 700 /var/lib/raptor-ecs-ax/downloads
cd /var/lib/raptor-ecs-ax/downloads
base="https://github.com/k3s-io/k3s/releases/download/${version}"
curl --retry 4 -fL "${base}/k3s" -o k3s
curl --retry 4 -fL "${base}/sha256sum-amd64.txt" -o sha256sum-amd64.txt
awk '$2 == "k3s" { print }' sha256sum-amd64.txt > k3s.sha256
[[ -s k3s.sha256 ]]
sha256sum -c k3s.sha256
install -m 755 k3s /usr/local/bin/k3s
ln -sf /usr/local/bin/k3s /usr/local/bin/kubectl
swapoff -a
modprobe overlay
modprobe br_netfilter
printf 'overlay\nbr_netfilter\n' > /etc/modules-load.d/raptor-k3s.conf
cat > /etc/sysctl.d/90-raptor-k3s.conf <<'CONFIG'
net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
CONFIG
sysctl --system >/dev/null
install -d -m 700 /etc/rancher/k3s
cat > /etc/rancher/k3s/config.yaml <<'CONFIG'
node-name: raptor-ecs-ax
cluster-cidr: 10.76.0.0/16
service-cidr: 10.77.0.0/16
write-kubeconfig-mode: "0600"
tls-san:
  - localhost
disable:
  - traefik
  - servicelb
kubelet-arg:
  - serialize-image-pulls=false
CONFIG
cat > /etc/systemd/system/k3s.service <<'UNIT'
[Unit]
Description=Raptor POC Kubernetes 1.37
After=network-online.target
Wants=network-online.target
[Service]
Type=notify
ExecStart=/usr/local/bin/k3s server
KillMode=process
Delegate=yes
LimitNOFILE=1048576
TasksMax=infinity
Restart=always
RestartSec=5
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now k3s
for i in $(seq 1 60); do
  if kubectl get node raptor-ecs-ax >/dev/null 2>&1; then break; fi
  sleep 5
done
kubectl wait --for=condition=Ready node/raptor-ecs-ax --timeout=180s
kubectl get nodes -o wide
kubectl get --raw /apis/certificates.k8s.io/v1
