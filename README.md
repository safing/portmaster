# Portmaster

The Portmaster enables you to protect your data on your device. You are back in charge of your outgoing connections: you choose what data you share and what data stays private. Read more on [docs.safing.io](http://docs.safing.io/).

## Current Status

The Portmaster is currently in alpha. Expect dragons.  
Supported platforms:

- linux_amd64
- windows_amd64 (_soon_)
- darwin_amd64 (_later_)

## Using the Alpha Version

#### Must-Know Basics

The Portmaster is all about protecting your privacy. As soon as it starts, it will start to intercept network connections. If other programs are already running, this may cause them to lose Internet connectivity for a short duration.

The main way to configure the application firewall is by configuring application profiles. For every program that is active on the network the Portmaster automatically creates a profile for it the first it's seen. These profiles are empty at first and only fed by a fallback profile. By changing these profiles in the app, you change what programs are allowed to do.

You can also see what is going on right now. The monitor page in the app lets you see the network like the Portmaster sees it: `Communications` represent a logical connection between a program and a domain. These second level objects group `Links` (physical connections: IP->IP) together for easier handling and viewing.

The Portmaster consists of three parts:
- The _core_ (ie. the _daemon_) that runs as an administrator and does all the work. (`sudo ./pmctl run core --db=/opt/pm_db`)
- The _app_, a user interface to set preferences, monitor apps and configure application profiles (`sudo ./pmctl run app --db=/opt/pm_db`)
- The _notifier_, a little menu/tray app for quick access and notifications (`sudo ./pmctl run notifier --db=/opt/pm_db`)

If you want to know more, here are [the docs](http://docs.safing.io/).

#### Installation

The `pmctl` command will help you get up and running. It will bootstrap your the environment and download additional files it needs. All commands need the `--db` parameter with the database location, as this is where all the data and also the binaries live.

Just download `pmctl` from the [releases page](https://github.com/Safing/portmaster/releases) and put it somewhere comfortable. You may freely choose where you want to put the database - it needs to be the same for all commands. Here we go - run every command in a seperate terminal window:

    # start the portmaster:
    sudo ./pmctl run core --db=/opt/pm_db
    # this will add some rules to iptables for traffic interception via nfqueue (and will clean up afterwards!)
    # already active connections may not be handled correctly, please restart programs for clean behavior

    # then start the app:
    ./pmctl run app -db=/opt/pm_db

    # and the notifier:
    ./pmctl run notifier -db=/opt/pm_db

#### Feedback

We'd love to know what you think, drop by on [our forum](https://discourse.safing.community/) and let us know!  
If you want to report a bug, please [open an issue on Github](https://github.com/Safing/portmaster/issues/new).

## Documentation

Documentation _in progress_ can be found here: [docs.safing.io](http://docs.safing.io/)

## Usage Dependencies

#### Linux
- libnetfilter_queue
  - debian/ubuntu:  `sudo apt-get install libnetfilter-queue1`
  - fedora:         `sudo yum install libnetfilter_queue`
  - arch:           `sudo pacman -S libnetfilter_queue`
- [Network Manager](https://wiki.gnome.org/Projects/NetworkManager) (_optional_)

#### Windows
- Windows 7 (with update KB3033929) or up
  - [KB3033929](https://docs.microsoft.com/en-us/security-updates/SecurityAdvisories/2015/3033929) (a 2015 security update) is required for correctly verifying the driver signature
- Windows Server 2016 systems must have secure boot disabled. (_clarification needed_)

## Build Dependencies

#### Linux
- libnetfilter_queue development files
  - debian/ubuntu:  `sudo apt-get install libnetfilter-queue-dev`
  - fedora:         `?`
  - arch:           `?`

## TCP/UDP Ports

The Portmaster (with Gate17) uses the following ports:
- ` 17` Gate17 port for connecting to Gate17 nodes
- ` 53` DNS server (local only)
- `717` Gate17 entrypoint as the local endpoint for tunneled connections (local only)
- `817` Portmaster API for integration with UI elements and other helpers (local only)

Learn more about [why we chose these ports](https://docs.safing.io/docs/portmaster/os-integration.html).

Gate17 nodes additionally uses other common ports like `80` and `443` to provide access in restricted network environments.
