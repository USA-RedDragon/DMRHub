# OpenBridge peers

OpenBridge links DMRHub to another network server, such as another DMRHub, HBlink or FreeDMR. Each link is a peer. You must be an admin to add a peer.

## Turn on OpenBridge

1. Go to **Admin**, then **Setup**, and find the **OpenBridge Settings** card in **DMR Settings**.
2. Turn on **Enabled**. Set the **Port** (default 62035, UDP) and, for FreeDMR peers, the **Server ID** (see [Enhanced peers](#enhanced-peers-for-freedmr)).
3. Click **Save**.

The configuration keys are `dmr.openbridge.enabled`, `dmr.openbridge.bind`, `dmr.openbridge.port` and `dmr.openbridge.server-id`. Open the UDP port in your firewall.

## Add a peer

1. Go to **Admin**, then **Peers** (`<siteurl>/admin/peers`). The **Peers** item shows only when OpenBridge is on.
2. Click **Enroll New Peer**.
3. Fill in the form:

   | Field | What to enter |
   | ----- | ------------- |
   | **Owner** | The user who owns the peer. The owner can see and change the peer on the **Peers** page in the main menu. |
   | **Peer ID** | The network ID of the link. Both ends of the link use the same ID. |
   | **IP Address** and **Port** | Where the other server listens for OpenBridge. DMRHub sends to this address. |
   | **Receive DMR traffic from this peer** | Turn on to take calls from the peer. |
   | **Transmit DMR traffic to this peer** | Turn on to send calls to the peer. |
   | **Use FreeDMR enhanced OpenBridge with this peer** | See [Enhanced peers](#enhanced-peers-for-freedmr). |
   | **This peer carries Codec2** | Turn on when the peer can take calls in Codec2. Off, DMRHub sends the peer AMBE only, and converts Codec2 calls when transcoding is on (see [Transcoding](transcoding.md)). |

4. Click **Save**. The browser shows the password of the peer. Copy it now: DMRHub does not show it again. Put it in the configuration of the other server as the passphrase of the link.

<!-- screenshot: admin-new-peer.png | /admin/peers/new filled in with peer ID 3000900, IP 192.0.2.10, port 62035, receive and transmit on -->

## Add the rules of a peer

A peer carries only the traffic that its rules allow. A new peer has no rules, so it carries nothing, even with **Receive** and **Transmit** on. The web interface does not have a page for rules yet. Add them with the API:

- A receive rule (`"direction": true`) lets in the calls from the peer to the destination IDs from `subject_id_min` to `subject_id_max`.
- A transmit rule (`"direction": false`) sends the peer the calls from the source radio IDs from `subject_id_min` to `subject_id_max`.

For example, to take the calls to talkgroups 1 to 999999 from peer 3000900, and to send it the calls of every radio:

```bash
curl -c cookies -H 'Content-Type: application/json' \
    -d '{"username":"<admin username>","password":"<password>"}' \
    <siteurl>/api/v1/auth/login
curl -b cookies -H 'Content-Type: application/json' \
    -d '{"direction":true,"subject_id_min":1,"subject_id_max":999999}' \
    <siteurl>/api/v1/peers/3000900/rules
curl -b cookies -H 'Content-Type: application/json' \
    -d '{"direction":false,"subject_id_min":1,"subject_id_max":4294967295}' \
    <siteurl>/api/v1/peers/3000900/rules
```

`GET <siteurl>/api/v1/peers/<peer ID>/rules` lists the rules, and `DELETE <siteurl>/api/v1/peers/<peer ID>/rules/<rule ID>` deletes one. The rules take effect at once, on every replica.

## Enhanced peers for FreeDMR

Turn on **Use FreeDMR enhanced OpenBridge with this peer** for a FreeDMR server whose link to this hub has `ENHANCED_OBP: True`. Only an admin can change it. For an enhanced peer, DMRHub does what FreeDMR's `hblink.py` does on such a link:

- It sends DMRE version 5 packets in place of the 73-octet DMRD packets. Each packet carries the send time, the source server, the source repeater and the hop count.
- It keeps the source server and the source repeater of a call that came in from another enhanced peer, and adds one to the hop count.
- It drops a call that already crossed 20 links (FreeDMR's `MAX_FBP_HOPS`) and asks the sender with a BCSQ to stop that stream.
- It sends a BCKA keepalive every 10 seconds. FreeDMR stops sending to a peer that sent no keepalive for 60 seconds.

The source server of a call from a local repeater is the **Server ID** in the OpenBridge settings, or the network ID of the peer when the server ID is 0. The server ID must be 0, or 4 to 7 digits. FreeDMR checks a 4 or 5-digit server ID against its list of server IDs. Other peers keep getting OpenBridge version 1.

## Several replicas

With several replicas, one replica sends all OpenBridge traffic, so that the peers see one source address. That replica holds a lease in Redis. When it stops, another replica takes over at once. When it dies, another replica takes over within 2 seconds. DMRHub drops the traffic in that gap, and does not hold it. A BCSQ from a peer applies on every replica, whichever replica got it.
