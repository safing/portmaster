# Testing SPN

This page documents ways to test if the SPN works as intended.

⚠ Work in Progress. Currently we are just collecting helpful things we find.

## Test Multi-Identity Routing

In order to test if the multi-identity routing is working, you can request multiple websites to display your public IP.
If they show different values, multi-identity routing is working.

### Websites

- <https://icanhazip.com>
- <https://ipecho.net>
- <https://ipinfo.io>
- <https://ipinfo.tw>

### Terminal

```sh
curl https://icanhazip.com
curl https://ipecho.net/plain
curl https://ipinfo.io/ip
curl https://ipinfo.tw/ip
```
