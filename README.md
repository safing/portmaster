# Portmaster

The Portmaster enables you to protect your data on your device. You are back in charge of your outgoing connections: you choose what data you share and what data stays private.

## Current Status

The Portmaster is currently in alpha. Expect dragons.  
Supported platforms:

- linux_amd64
- windows_amd64 (_soon_)
- darwin_amd64 (_later_)

## Usage

Just download the portmaster from the releases page.

    ./portmaster -db=/opt/pm_db
    # this will add some rules to iptables for traffic interception via nfqueue (and will clean up afterwards!)

    # then start the ui
    ./portmaster -db=/opt/pm_db -ui
    # missing files will be automatically download when first needed

## Documentation

Documentation _in progress_ can be found here: [http://docs.safing.io/](http://docs.safing.io/)

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
