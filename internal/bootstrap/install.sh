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

# Cloud.ru images switch apt to the platform mirror at first boot (bootcmd
# "configure-apt-mirrors" in the image template) only after checking the
# mirror answers. A VM gets its floating IP after boot, so the check can
# fail and leave the image's stale sources, and get.docker.com then dies on
# "apt-get update". With the network up now, run the platform's own step
# again. The marker is the one that template writes on success.
mirror_marker=/var/lib/cloud/instance/sem/configure-apt-mirrors
apt_ready() {
	apt-get update -q >/tmp/evo-apt.log 2>&1
}
if ! docker compose version >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1 && ! apt_ready; then
	echo "apt-get update failed:"
	grep -E '^(W|E):' /tmp/evo-apt.log | head -5 || true
	if [ ! -f "$mirror_marker" ] && command -v cloud-init >/dev/null 2>&1; then
		echo "re-running the image's apt mirror setup (cloud-init bootcmd)"
		cloud-init single --name bootcmd --frequency always || true
	fi
	if ! apt_ready; then
		echo "apt-get update still fails; apt sources:"
		grep -rhE '^(deb |URIs:)' /etc/apt/sources.list /etc/apt/sources.list.d 2>/dev/null | head -5 || true
		grep -E '^(W|E):' /tmp/evo-apt.log | head -5 || true
		exit 1
	fi
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
