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
UI_DROPIN=${DROPIN_DIR}/60-ui-rust.conf
RUST_BIN=${BIN_DIR}/portmaster-core-rs
# Set by backup() so a rollback or a later `go` can point at the last backup.
BACKUP_PATH=""

usage() {
	cat <<EOF
Usage: $0 status
       $0 rust [path/to/portmaster-core-rs] [--yes]
       $0 go
       $0 ui {status|rust|typescript}

status  show which core the service uses
rust    install the Rust core (if a path is given) and switch the service to it
go      switch the service back to the Go core
ui      switch the UI between the TypeScript (Angular) build and the Rust/WASM pilot
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
	local candidate newest=""
	[[ -d ${DATA_DIR}/core-switch-backup ]] || return 0
	for candidate in "${DATA_DIR}"/core-switch-backup/*/; do
		[[ -d ${candidate} ]] && newest=${candidate%/}
	done
	printf '%s' "${newest}"
	return 0
}

# API port can be overridden by core/listenAddress in config.json.
api_port() {
	local port
	port=$(python3 -c 'import json,os,sys;d=json.load(open(os.argv[1]));print(str(d.get("core",{}).get("listenAddress","127.0.0.1:817")).rsplit(":",1)[-1])' "${DATA_DIR}/config.json" 2>/dev/null) || port=""
	printf '%s' "${port:-817}"
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
	local i url="http://127.0.0.1:$(api_port)/api/v1/ready"
	for i in $(seq 1 10); do
		if curl -fsS --max-time 2 "${url}" >/dev/null 2>&1; then
			echo "ready: $(curl -fsS --max-time 2 "${url}")"
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
		# The Rust core may already have rewritten these files; restore the
		# pre-switch snapshot before starting Go again.
		if [[ -f ${BACKUP_PATH}/config.json ]]; then
			cp -a "${BACKUP_PATH}/config.json" "${DATA_DIR}/config.json"
		fi
		if [[ -d ${BACKUP_PATH}/core ]]; then
			rm -rf "${DATA_DIR}/databases/core"
			cp -a "${BACKUP_PATH}/core" "${DATA_DIR}/databases/core"
		fi
		echo "restored config.json and databases/core from ${BACKUP_PATH}" >&2
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
ui)
	need_root "$@"
	shift
	case ${1:-status} in
	status)
		if [[ -f ${UI_DROPIN} ]]; then
			echo "ui: rust (WASM pilot)"
		else
			echo "ui: typescript (Angular)"
		fi
		;;
	rust)
		mkdir -p "${DROPIN_DIR}"
		cat >"${UI_DROPIN}" <<UIOUF
[Service]
Environment=PORTMASTER_UI_TYPE=rust
UIOUF
		restart
		wait_ready
		echo "UI switched to the Rust/WASM pilot at / (TypeScript UI still available on the Go core)"
		;;
	typescript)
		rm -f "${UI_DROPIN}"
		restart
		wait_ready
		echo "UI switched back to the TypeScript (Angular) build"
		;;
	*)
		echo "usage: $0 ui {status|rust|typescript}" >&2
		exit 2
		;;
	esac
	;;
*)
	usage
	exit 2
	;;
esac
