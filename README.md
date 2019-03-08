# Portmaster

The Portmaster enables you to protect your data on your device. You are back in charge of your outgoing connections: you choose what data you share and what data stays private. Read more on [docs.safing.io](http://docs.safing.io/).

## Current Status

The Portmaster is currently in alpha. Expect dragons.  
Supported platforms:

- linux_amd64
- windows_amd64 (_soon_)
- darwin_amd64 (_later_)

## Usage

If you do not already know, please read about [how the Portmaster works](http://docs.safing.io/).

You can download the Portmaster from the [releases page](https://github.com/Safing/portmaster/releases):

    # print help for startup options:
    ./portmaster -help
    # (preferences can be configured using the UI)

    # start the portmaster:
    sudo ./portmaster -db=/opt/pm_db
    # this will add some rules to iptables for traffic interception via nfqueue (and will clean up afterwards!)
    # already active connections may not be handled correctly, please restart programs for clean behavior

    # then start the ui:
    ./portmaster -db=/opt/pm_db -ui
    # missing files will be automatically downloaded when first needed

    # and the notifier:
    ./portmaster -db=/opt/pm_db -notifier

## Documentation

Documentation _in progress_ can be found here: [docs.safing.io](http://docs.safing.io/)

## Dependencies

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
