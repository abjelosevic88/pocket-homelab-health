# HTTPS over a VPN (Tailscale, WireGuard)

This applies to every Pocket app and Pocket Core, not only Health; the
general guide is [Reach your apps away from home](https://pockethomelab.com/docs/getting-started/away-from-home).

The iPhone app sends Apple Health in the background whenever the phone
records something new. Away from home that only works if the phone can reach
Pocket Health over **https://**.

## Why plain http stops working away from home

iOS lets an app use plain `http://` only for addresses on the local network:
private addresses such as `192.168.1.20` or `10.0.0.5`, and names ending in
`.local`. A VPN address is not local to iOS. That covers Tailscale's
`100.x.y.y`, a MagicDNS name like `nas.tail1234.ts.net`, and a WireGuard
address outside the private ranges. iOS refuses those requests before they
leave the phone. This is an iOS rule (App Transport Security), and an app
cannot make an exception for an IP range.

The fix is a real certificate for a name the phone can reach over the VPN.
Pick one of the two ways below.

## Option 1: Tailscale Serve (the shortest way)

Tailscale gets a certificate for the machine's `ts.net` name and proxies to
Pocket Health. Nothing is opened to the internet.

1. In the Tailscale admin console under **DNS**, turn on **MagicDNS** and
   **HTTPS Certificates**.
2. On the machine that runs Pocket Health (the port is the one published by
   `docker-compose.yml`, 8080 by default):

   ```sh
   tailscale serve --bg 8080
   tailscale serve status    # shows https://<machine>.<tailnet>.ts.net
   ```

3. Use that `https://…ts.net` address everywhere (see "Then" below).

The certificate is public in Certificate Transparency logs, so the machine
and tailnet names are visible there. Its contents and the traffic are not.

## Option 2: your own domain with Caddy, reachable only over the VPN

This works with any VPN. It needs a domain whose DNS you can change.

1. Add a DNS record `health.example.com` pointing to the server's **VPN**
   address (for example `100.101.102.103`). From the internet that address
   leads nowhere; on the VPN it reaches the server.
2. The server is not reachable from the internet, so Caddy has to prove the
   domain through DNS (the ACME DNS challenge). Build Caddy with your DNS
   provider's module (for example `xcaddy build --with
   github.com/caddy-dns/cloudflare`) and give it an API token that can edit the
   zone:

   ```caddy
   health.example.com {
   	tls {
   		dns cloudflare {env.CF_API_TOKEN}
   	}
   	request_body {
   		max_size 4GB
   	}
   	reverse_proxy pocket-health:8080
   }
   ```

   The large body limit is for Apple Health export uploads; see
   [reverse-proxy.md](reverse-proxy.md).

A self-signed certificate also works, but only after every phone installs it
and turns it on under Settings → General → About → Certificate Trust
Settings. Option 1 or 2 is less work.

## Then

- On the server, set `BASE_URL=https://…` (the new address) and
  `SECURE_COOKIES=true`, then restart.
- In the iPhone app, change the Pocket Health address to the `https://` one.
  The token stays the same.
- **Keep the VPN on in the background.** iOS wakes the app when Apple Health
  has something new, but the request can only reach the server if the VPN is
  connected at that moment. In the Tailscale app, turn on **VPN On Demand**
  (or use an always-on WireGuard profile).
- Check it: away from home, record something (a few steps are enough), wait a
  little, and look at **Settings → Phone & imports** in Pocket Health. The
  phone's last push should be recent.
