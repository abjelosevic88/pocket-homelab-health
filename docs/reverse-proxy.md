# Reverse proxy

Set `BASE_URL` to the public address and `SECURE_COOKIES=true` behind HTTPS. Raise the proxy's body limit for Apple Health exports (gigabytes) — see [deploy/reverse-proxy/Caddyfile](../deploy/reverse-proxy/Caddyfile). `/api/*` is reachable for the phone, Homepage widgets and shortcuts; they send their own bearer token.
