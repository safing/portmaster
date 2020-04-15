# Portmaster Wiki

The Portmaster is a privacy app that at its core simply intercepts _all_ your network connections. Different modules with different privacy features are built on top of it, which can all be enabled or disabled as desired:

![portmaster modules](https://safing.io/assets/img/portmaster/modules.png)

#### ⚠️ Disclaimer

> The Portmaster is still in its early "pre-alpha" development stage. It is functional, but has not yet been tested widely. We are glad if you want to try out the Portmaster right away but please expect bugs and rather technical problems. We'll push updates and fixes as we go. A list of known problems can be found at the bottom of this page.

#### 🔄 Automatic Updates

We have set up update servers so we can push fixes and improvements as we go.

# Modules

## DNS-over-TLS Resolver

**Status:** _pre-alpha_

A DNS resolver that does not only encrypt your queries, but figures out where it makes the most sense to send your queries. Queries for local domains will not be sent to the upstream servers. This means it won't break your or your company's network setup.

**Features/Settings:**

- Configure upstream DNS resolvers
- Don't use assigned Nameserver (by DHCP / local network - public WiFi!)
- Don't use Multicast DNS (public WiFi!)

## Privacy Filter

**Status:** _unreleased - pre-alpha scheduled for the next days_

Think of a pi-hole for your computer. Or an ad-blocker that blocks ads on your whole computer, not only on your browser. With you everywhere you go and every network you visit.

**Features/Settings:**

- Select and activate block-lists
- Manually black/whitelist domains
  - You can whitelist domains in case something breaks
- CNAME Blocking (block these new nasty "unblockable" ads/trackers - coming soon)
- Block all subdomains of a domain in the block-lists

## Safing Privacy Network (SPN)

**Status:** _unreleased - pre-alpha scheduled for June_

Please [visit our Kickstarter campaign](https://www.kickstarter.com/projects/safingio/spn/) to read all about this module.

# Installation

Installation instructions for your platform as well as known issues can be found at the respective wiki pages:

- [Linux](https://github.com/safing/portmaster/wiki/Linux)
- [Windows](https://github.com/safing/portmaster/wiki/Windows)
