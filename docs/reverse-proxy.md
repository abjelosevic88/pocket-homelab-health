# Reverse proxy

Set `BASE_URL` to the public address and `SECURE_COOKIES=true` behind HTTPS. Raise the proxy's body limit for Apple Health exports (gigabytes) — see [deploy/reverse-proxy/Caddyfile](../deploy/reverse-proxy/Caddyfile). `/api/*` is reachable for the phone, Homepage widgets and shortcuts; they send their own bearer token.

Only reachable over a VPN (Tailscale, WireGuard)? iOS refuses plain http to a VPN address, so the phone's background sync needs https there too: [https-over-vpn.md](https-over-vpn.md).
