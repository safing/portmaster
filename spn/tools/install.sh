#!/bin/sh
#
# This script should be run via curl as root:
#   sudo sh -c "$(curl -fsSL https://raw.githubusercontent.com/safing/portmaster/master/spn/tools/install-spn.sh)"
# or wget
#   sudo sh -c "$(wget -qO- https://raw.githubusercontent.com/safing/portmaster/master/spn/tools/install-spn.sh)"
#
# As an alternative, you can first download the install script and run it afterwards:
#   wget https://raw.githubusercontent.com/safing/portmaster/master/spn/tools/install-spn.sh
#   sudo sh ./install.sh
#
#
set -e

ARCH=
INSTALLDIR=
PMSTART=
ENABLENOW=
INSTALLSYSTEMD=
SYSTEMDINSTALLPATH=

apply_defaults() {
    ARCH=${ARCH:-amd64}
    INSTALLDIR=${INSTALLDIR:-/opt/safing/spn}
    PMSTART=${PMSTART:-https://updates.safing.io/latest/linux_${ARCH}/start/portmaster-start}
    SYSTEMDINSTALLPATH=${SYSTEMDINSTALLPATH:-/etc/systemd/system/spn.service}

    if command_exists systemctl; then
        INSTALLSYSTEMD=${INSTALLSYSTEMD:-yes}
        ENABLENOW=${ENABLENOW:-yes}
    else
        INSTALLSYSTEMD=${INSTALLSYSTEMD:-no}
        ENABLENOW=${ENABLENOW:-no}
    fi

    # The hostname may be freshly set, ensure the ENV variable is correct.
    export HOSTNAME=$(hostname)
}

command_exists() {
    command -v "$@" >/dev/null 2>&1
}

setup_tty() {
    if [ -t 0 ]; then
        interactive=yes
    fi

	if [ -t 1 ]; then
		RED=$(printf '\033[31m')
		GREEN=$(printf '\033[32m')
		YELLOW=$(printf '\033[33m')
		BLUE=$(printf '\033[34m')
		BOLD=$(printf '\033[1m')
		RESET=$(printf '\033[m')
	else
		RED=""
		GREEN=""
		YELLOW=""
		BLUE=""
		BOLD=""
		RESET=""
	fi
}

log() {
    echo ${GREEN}${BOLD}"-> "${RESET}"$@" >&2
}

error() {
    echo ${RED}"Error: $@"${RESET} >&2
}

warn() {
    echo ${YELLOW}"warn: $@"${RESET} >&2
}

run_systemctl() {
    systemctl $@ >/dev/null 2>&1 
}

download_file() {
    local src=$1
    local dest=$2

    if command_exists curl; then
        curl --silent --fail --show-error --location --output $dest $src
    elif command_exists wget; then
        wget --quiet -O $dest $src
    else
        error "No suitable download command found, either curl or wget must be installed"
        exit 1
    fi
}

ensure_install_dir() {
    log "Creating ${INSTALLDIR}"
    mkdir -p ${INSTALLDIR}
}

download_pmstart() {
    log "Downloading portmaster-start ..."
    local dest="${INSTALLDIR}/portmaster-start"
    if [ -f "${dest}" ]; then
        warn "Overwriting existing portmaster-start at ${dest}"
    fi

    download_file ${PMSTART} ${dest}

    log "Changing permissions"
    chmod a+x ${dest}
}

download_updates() {
    log "Downloading updates ..."
    ${INSTALLDIR}/portmaster-start --data=${INSTALLDIR} update
}

setup_systemd() {
    log "Installing systemd service unit ..."
    if [ ! "${INSTALLSYSTEMD}" = "yes" ]; then
        warn "Skipping setup of systemd service unit"
        echo "To launch the hub, execute the following as root:"
        echo ""
        echo "${INSTALLDIR}/portmaster-start --data ${INSTALLDIR} hub"
        echo ""
        return
    fi

    if [ -f "${SYSTEMDINSTALLPATH}" ]; then
        warn "Overwriting existing unit path"
    fi

    cat >${SYSTEMDINSTALLPATH} <<EOT
[Unit]
Description=Safing Privacy Network Hub
Wants=nss-lookup.target
Conflicts=shutdown.target
Before=shutdown.target

[Service]
Type=simple
Restart=on-failure
RestartSec=5
LimitNOFILE=infinity
Environment=LOGLEVEL=warning
Environment=SPN_ARGS=
EnvironmentFile=-/etc/default/spn
ExecStart=${INSTALLDIR}/portmaster-start --data ${INSTALLDIR} hub -- --log \$LOGLEVEL \$SPN_ARGS

[Install]
WantedBy=multi-user.target
EOT

    log "Reloading systemd unit files"
    run_systemctl daemon-reload

    if run_systemctl is-active spn ||
       run_systemctl is-failed spn; then
        log "Restarting SPN hub"
        run_systemctl restart spn.service
    fi

    # TODO(ppacher): allow disabling enable
    if ! run_systemctl is-enabled spn ; then
        if [ "${ENABLENOW}" = "yes" ]; then
            log "Enabling and starting SPN."
            run_systemctl enable --now spn.service || exit 1

            log "Watch logs using: journalctl -fu spn.service"
        else
            log "Enabling SPN"
            run_systemctl enable spn.service || exit 1
        fi
    fi

}

ask_config() {
    if [ "${HOSTNAME}" = "" ]; then
        log "Please enter hostname:"
        read -p "> " HOSTNAME
    fi
    if [ "${METRICS_COMMENT}" = "" ]; then
        log "Please enter metrics comment:"
        read -p "> " METRICS_COMMENT
    fi
}

write_config_file() {
    cat >${1} <<EOT
{
  "core": {
    "metrics": {
      "instance": "$HOSTNAME",
      "comment": "$METRICS_COMMENT",
      "push": "$PUSHMETRICS"
    }
  },
  "spn": {
    "publicHub": {
      "name": "$HOSTNAME"
    }
  }
}
EOT
}

confirm_config() {
    log "Installation configuration:"
    echo ""
    echo "   Architecture: ${BOLD}${ARCH}${RESET}"
    echo "   Download-URL: ${BOLD}${PMSTART}${RESET}"
    echo "     Target Dir: ${BOLD}${INSTALLDIR}${RESET}"
    echo "Install systemd: ${BOLD}${INSTALLSYSTEMD}${RESET}"
    echo "      Unit path: ${BOLD}${SYSTEMDINSTALLPATH}${RESET}"
    echo "      Start Now: ${BOLD}${ENABLENOW}${RESET}"
    echo ""
    echo "         Config:"
    tmpfile=$(mktemp)
    write_config_file $tmpfile
    cat $tmpfile
    echo ""
    echo ""

    if [ ! -z "${interactive}" ]
    then
        read -p "Continue (Y/n)? " ans
        case "$ans" in
            "" | "y" | "Y")
                echo ""
                ;;
            **)
                error "User aborted"
                exit 1
        esac
    fi
}

print_help() {
    cat <<EOT
Usage: $0 [OPTIONS...]

${BOLD}Options:${RESET}
    ${GREEN}-y, --unattended${RESET}           Don't ask for confirmation.
    ${GREEN}-n, --no-start${RESET}             Do not immediately start SPN hub.
    ${GREEN}-t, --target PATH${RESET}          Configure the installation directory.
    ${GREEN}-h, --help${RESET}                 Display this help text
    ${GREEN}-a, --arch${RESET}                 Configure the binary architecture.
    ${GREEN}-u, --url URL${RESET}              Set download URL for portmaster start.
    ${GREEN}-S, --no-systemd${RESET}           Do not install systemd service unit.
    ${GREEN}-s, --service-path PATH${RESET}    Location for the systemd unit file.
EOT
}

main() {
    setup_tty

    # Parse arguments
    while [ $# -gt 0 ]
    do
        case $1 in
            --unattended | -y)
                interactive=""
                ;;
            --no-start | -n)
                ENABLENOW="no"
                ;;
            --target | -t)
                INSTALLDIR=$2
                shift
                ;;
            --help | -h)
                print_help
                exit 1 ;;
            --arch | -a)
                ARCH=$2
                shift
                ;;
            --url | -u)
                PMSTART=$2
                shift
                ;;
            --no-systemd | -S)
                INSTALLSYSTEMD=no
                ENABLENOW=no
                ;;
            --service-path | -s)
                SYSTEMDINSTALLPATH=$2
                shift
                ;;
            *)
                error "Unknown flag $1"
                exit 1
                ;;
        esac
        shift
    done

    cat <<EOT
${BLUE}${BOLD}
          ▄▄▄▄  ▄▄▄▄▄  ▄▄   ▄
         █▀   ▀ █   ▀█ █▀▄  █
         ▀█▄▄▄  █▄▄▄█▀ █ █▄ █
             ▀█ █      █  █ █
         ▀▄▄▄█▀ █      █   ██
        ${GREEN}Safing Privacy Network
${RESET}
EOT

    # prepare config
    apply_defaults
    ask_config
    confirm_config

    # Setup hub
    ensure_install_dir
    download_pmstart
    download_updates
    write_config_file "${INSTALLDIR}/config.json"

    # setup systemd
    setup_systemd
}

main "$@"
