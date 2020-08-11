# Portmaster

The Portmaster is a privacy app that at its core simply intercepts _all_ your network connections. Different modules with different privacy features are built on top of it, which can all be enabled or disabled as desired:

![portmaster modules](https://safing.io/assets/img/portmaster/modules.png)

#### ⚠️ Disclaimer

> The Portmaster is still in its early "alpha" development stage. While some features might still have bugs, it runs quite stable and can easily be uninstalled again. We'll push updates and fixes as we go. A list of known problems can be found at the bottom of this page.

#### 🔄 Automatic Updates

We have set up update servers so we can push fixes and improvements as we go.

# Modules

## DNS-over-TLS Resolver

**Status:** _alpha_

A DNS resolver that does not only encrypt your queries, but figures out where it makes the most sense to send your queries. Queries for local domains will not be sent to the upstream servers. This means it won't break your or your company's network setup.

**Features/Settings:**

- Configure upstream DNS resolvers
- Don't use assigned Nameserver (by DHCP / local network - public WiFi!)
- Don't use Multicast DNS (public WiFi!)

## Privacy Filter

**Status:** _alpha_

Think of a pi-hole for your computer. Or an ad-blocker that blocks ads on your whole computer, not only on your browser. With you everywhere you go and every network you visit.

**Features/Settings:**

- Select and activate block-lists
- Manually block/allow domains
  - You can allow domains in case something breaks
- CNAME Blocking (block these new nasty "unblockable" ads/trackers)
- Block all subdomains of a domain in the block-lists

## Safing Privacy Network (SPN)

**Status:** _currently in closed pre-alpha_

[Visit our homepage](https://safing.io/spn/) or [its Kickstarter campaign](https://www.kickstarter.com/projects/safingio/spn) to read all about this module.

# Installation

Installation instructions for your platform as well as known issues can be found at the respective wiki pages:

- [Linux](https://github.com/safing/portmaster/wiki/Linux)
- [Windows](https://github.com/safing/portmaster/wiki/Windows)

# Sceenshot Tour

Please note that so far only the dashboard has gotten attention from our designer.
All views will be updated with proper UX.

![Screenshot Tour #1](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-1.png)
![Screenshot Tour #2](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-2.png)
![Screenshot Tour #3](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-3.png)
![Screenshot Tour #4](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-4.png)
![Screenshot Tour #5](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-5.png)
![Screenshot Tour #6](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-6.png)
![Screenshot Tour #7](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-7.png)
![Screenshot Tour #8](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-8.png)
![Screenshot Tour #9](https://assets.safing.io/portmaster/tours/portmaster-screenshot-tour-9.png)
