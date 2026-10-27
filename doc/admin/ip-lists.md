# IP lists

The IP lists refuse the packets of some source addresses, or of every address but some. DMRHub checks the source address of each packet before any other work on it.

There are two lists:

- **Hotspots and repeaters** (`dmr.ip-lists.repeaters`): MMDVM hotspots and repeaters, browser radios over WebTransport, and IPSC repeaters.
- **OpenBridge peers** (`dmr.ip-lists.peers`): other servers that link over OpenBridge.

Each list has a mode:

| Mode | What it does |
| ---- | ------------ |
| `off` | Accepts every address. This is the default. |
| `block` | Refuses the addresses of the block list. |
| `allow` | Refuses every address but those of the allow list. An empty allow list refuses every address. |

Only the list of the mode applies: the block mode does not read the allow list, and the allow mode does not read the block list. Each list keeps its entries when you change the mode.

An entry is an IPv4 or IPv6 address, such as `203.0.113.7` or `2001:db8::1`, or a CIDR range, such as `198.51.100.0/24` or `2001:db8::/32`. A range must have no bits set after its prefix: write `10.0.0.0/8`, not `10.1.2.3/8`. An IPv4 address in IPv6 form, such as `::ffff:203.0.113.7`, is the IPv4 address, also when a hotspot connects to a dual-stack listener.

## What a refused address gets

| Protocol | What happens |
| -------- | ------------ |
| MMDVM | A login, a config or a keepalive gets `MSTNAK`, as for a repeater that the server does not know. DMRHub drops the other packets without an answer. |
| WebTransport | The browser gets HTTP status 403 when it opens the session. |
| IPSC | DMRHub drops the packets without an answer, as for a peer that it does not know. It sends nothing to the address. |
| OpenBridge | DMRHub drops the packets from the address. It also sends no calls and no keepalives to a peer whose **IP Address** the list refuses. |

DMRHub logs a refused address at the info level, at most once a minute for each address. The metric `ingress_dropped_bursts_total` counts the refused packets with the reason `ip_list`.

## Changes

A change in Setup goes into effect at once on every replica. Each replica ends the sessions of the addresses that the changed list refuses: an MMDVM hotspot gets `MSTCL`, and a browser radio loses its session. An IPSC repeater stays registered until it times out, but DMRHub drops its packets and sends it nothing from the change on.

## OpenBridge peers and their IP address

Each peer has its own **IP Address** on the **Peers** page. DMRHub sends the calls of the peer to that address, and logs a warning when a packet of the peer comes from another address. The IP address of a peer does not refuse packets: the HMAC of the peer password checks the packets.

The list of the peers applies before the peer is known, to the source address of each packet, and to the **IP Address** of each peer that DMRHub sends to. To accept only the peers that you added, set the mode of the peers to `allow` and list the IP address of each peer.

## Set the lists

Set the lists on the **IP lists** area of the Setup page, or with the config API: `PUT /api/v1/config` with the whole config, as the Setup page sends it. The lists are under `dmr.ip-lists`. A save with an entry that does not parse fails with status 400 and the field of the list, such as `dmr.ip-lists.repeaters.block`.

You can also set the lists in the config file, in the environment, such as `DMR_IP_LISTS_REPEATERS_MODE=block`, or with flags. A list set there is locked, and Setup cannot change it. See [Configuration](configuration.md). For example:

```yaml
dmr:
  ip-lists:
    repeaters:
      mode: block
      block:
        - 203.0.113.7
        - 198.51.100.0/24
        - 2001:db8:bad::/48
    peers:
      mode: allow
      allow:
        - 192.0.2.10
        - 2001:db8:10::/64
```
