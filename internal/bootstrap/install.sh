#!/bin/sh
# Prepares a fresh VM for `docker compose up`. Runs as root (via sudo) over
# SSH, so it does not depend on the compute API accepting user-data: Cloud.ru
# renders cloud-init only from the image's own template. Idempotent: on a VM
# that already has Docker it only fixes the group and the app directory.
# $1 is the login user that runs docker compose.
set -eu
user="$1"

# First-boot cloud-init holds the apt lock (the platform template runs
# package_update). Its exit code is not trusted: warnings give 2.
if command -v cloud-init >/dev/null 2>&1; then
	cloud-init status --wait >/dev/null 2>&1 || true
fi

if ! docker compose version >/dev/null 2>&1; then
	mkdir -p /etc/docker
	if [ ! -f /etc/docker/daemon.json ]; then
		cat >/etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {"max-size": "10m", "max-file": "3"}
}
EOF
	fi
	if ! command -v curl >/dev/null 2>&1; then
		apt-get update -q && DEBIAN_FRONTEND=noninteractive apt-get install -y -q curl ca-certificates
	fi
	curl -fsSL https://get.docker.com | sh
fi

systemctl enable --now docker >/dev/null 2>&1 || true
usermod -aG docker "$user"
install -d -o "$user" /opt/app
docker compose version
