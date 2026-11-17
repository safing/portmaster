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
# Set by backup() so a rollback or a later `go` can point at the last backup.
BACKUP_PATH=""

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
	BACKUP_PATH=${dest}
	echo "backup: ${dest}"
}

# Newest backup directory (timestamps sort lexicographically). Empty if none.
newest_backup() {
	local candidate
	[[ -d ${DATA_DIR}/core-switch-backup ]] || return 0
	for candidate in "${DATA_DIR}"/core-switch-backup/*/; do
		[[ -d ${candidate} ]] && printf '%s' "${candidate%/}"
	done
	return 0
}

restart() {
	systemctl daemon-reload
	if ! systemctl restart "${UNIT}"; then
		echo "ERROR: failed to restart ${UNIT}" >&2
		return 1
	fi
	sleep 2
	systemctl --no-pager --lines=5 status "${UNIT}" || true
}

wait_ready() {
	local i
	for i in $(seq 1 10); do
		if curl -fsS --max-time 2 http://127.0.0.1:817/api/v1/ready >/dev/null 2>&1; then
			echo "ready: $(curl -fsS --max-time 2 http://127.0.0.1:817/api/v1/ready)"
			return 0
		fi
		sleep 1
	done
	echo "WARNING: Rust core did not become ready; check: journalctl -u ${UNIT} -n 50" >&2
	return 1
}

rollback() {
	echo "Rolling back to the Go core..." >&2
	rm -f "${DROPIN}"
	rmdir --ignore-fail-on-non-empty "${DROPIN_DIR}" || true
	restart || echo "WARNING: could not restart ${UNIT}; start it manually" >&2
	if [[ -n ${BACKUP_PATH} ]]; then
		echo "Backup of config.json and databases/core: ${BACKUP_PATH}" >&2
		echo "If the Go core cannot read the Rust-written config/database," >&2
		echo "restore them with: sudo cp -a ${BACKUP_PATH}/config.json ${DATA_DIR}/config.json" >&2
		echo "and: sudo cp -a ${BACKUP_PATH}/core ${DATA_DIR}/databases/core" >&2
	fi
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
		cat <<EOF
The Rust core now supports packet interception (iptables/nfqueue) and needs root
privileges via the existing systemd unit. UI and data formats are compatible;
config.json and the core database are backed up before switching.
EOF
		read -r -p "Switch anyway? [y/N] " answer
		[[ ${answer} == [yY] ]] || exit 1
	fi
	backup
	mkdir -p "${DROPIN_DIR}"
	cat >"${DROPIN}" <<EOF
[Service]
ExecStart=
ExecStart=${RUST_BIN} --bin-dir=${BIN_DIR} --data-dir=${DATA_DIR} --log-dir=${DATA_DIR}/log --log=\${LOGLEVEL}
EOF
	# Never leave a broken drop-in behind: if the restart fails or the core
	# does not become ready, roll back to the Go core automatically.
	if ! restart; then
		rollback
		exit 1
	fi
	if ! wait_ready; then
		rollback
		exit 1
	fi
	;;
go)
	need_root "$@"
	if [[ -f ${DROPIN} ]]; then
		rm -f "${DROPIN}"
		rmdir --ignore-fail-on-non-empty "${DROPIN_DIR}"
		# The Rust core may have rewritten config.json and the core database in
		# a form the Go core cannot read. Restore them from the newest
		# pre-switch backup so switching back is actually usable. Only do this
		# when we were really on Rust, never on an idempotent `go`.
		restore_src=$(newest_backup)
		if [[ -n ${restore_src} ]]; then
			if [[ -f ${restore_src}/config.json ]]; then
				cp -a "${restore_src}/config.json" "${DATA_DIR}/config.json"
				echo "restored config.json from ${restore_src}"
			fi
			if [[ -d ${restore_src}/core ]]; then
				rm -rf "${DATA_DIR}/databases/core"
				cp -a "${restore_src}/core" "${DATA_DIR}/databases/core"
				echo "restored databases/core from ${restore_src}"
			fi
		else
			echo "no core-switch-backup found; nothing to restore" >&2
		fi
	fi
	restart
	;;
*)
	usage
	exit 2
	;;
esac
