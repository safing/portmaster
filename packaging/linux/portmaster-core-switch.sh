#!/usr/bin/env bash
# Switches the Portmaster system service between the Go core and the Rust core.
# Both cores use the same data directory; a backup of config and core database
# is taken before every switch.
set -euo pipefail

BIN_DIR=/usr/lib/portmaster
DATA_DIR=/var/lib/portmaster
UNIT=portmaster.service
DROPIN_DIR=/etc/systemd/system/${UNIT}.d
DROPIN=${DROPIN_DIR}/50-rust-core.conf
RUST_BIN=${BIN_DIR}/portmaster-core-rs

usage() {
	cat <<EOF
Usage: $0 status
       $0 rust [path/to/portmaster-core-rs] [--yes]
       $0 go

status  show which core the service uses
rust    install the Rust core (if a path is given) and switch the service to it
go      switch the service back to the Go core
EOF
}

need_root() {
	if [[ ${EUID} -ne 0 ]]; then
		echo "run as root (sudo $0 $*)" >&2
		exit 1
	fi
}

current() {
	if [[ -f ${DROPIN} ]]; then echo rust; else echo go; fi
}

backup() {
	local dest="${DATA_DIR}/core-switch-backup/$(date +%Y%m%d-%H%M%S)"
	mkdir -p "${dest}"
	[[ -f ${DATA_DIR}/config.json ]] && cp -a "${DATA_DIR}/config.json" "${dest}/"
	[[ -d ${DATA_DIR}/databases/core ]] && cp -a "${DATA_DIR}/databases/core" "${dest}/"
	echo "backup: ${dest}"
}

restart() {
	systemctl daemon-reload
	systemctl restart "${UNIT}"
	sleep 2
	systemctl --no-pager --lines=5 status "${UNIT}" || true
}

cmd=${1:-}
case ${cmd} in
status)
	echo "service core: $(current)"
	[[ -x ${RUST_BIN} ]] && "${RUST_BIN}" --version || echo "rust core not installed at ${RUST_BIN}"
	;;
rust)
	need_root "$@"
	shift
	src="" yes=0
	for arg in "$@"; do
		case ${arg} in
		--yes) yes=1 ;;
		*) src=${arg} ;;
		esac
	done
	if [[ -n ${src} ]]; then
		install -m 0755 "${src}" "${RUST_BIN}"
	fi
	if [[ ! -x ${RUST_BIN} ]]; then
		echo "no Rust core at ${RUST_BIN}; pass the built binary path" >&2
		exit 1
	fi
	if [[ ${yes} -ne 1 ]]; then
		echo "The Rust core does not filter traffic yet (no packet interception)."
		read -r -p "Switch anyway? [y/N] " answer
		[[ ${answer} == [yY] ]] || exit 1
	fi
	backup
	mkdir -p "${DROPIN_DIR}"
	cat >"${DROPIN}" <<EOF
[Service]
ExecStart=
ExecStart=${RUST_BIN} --bin-dir=${BIN_DIR} --data-dir=${DATA_DIR} --log-dir=${DATA_DIR}/log
EOF
	restart
	;;
go)
	need_root "$@"
	if [[ -f ${DROPIN} ]]; then
		backup
		rm -f "${DROPIN}"
		rmdir --ignore-fail-on-non-empty "${DROPIN_DIR}"
	fi
	restart
	;;
*)
	usage
	exit 2
	;;
esac
