# Admin's Guide

This guide is for users of DMRHub who have been granted administrative permissions.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

If you are the server operator looking to deploy DMRHub, see the [Deployment Guide](Deployment-Guide.md) and [Configuration Guide](Configuration.md).

## The Administrator Role

The administrator role exists to share the burden of the manual user approval process and other user support actions. The first admin user is created during the [setup wizard](Configuration.md#setup-wizard-recommended) on initial deployment.

## Administrator Capabilities

DMRHub administrators can:

- List, edit, and delete any repeater
- Create and edit talkgroups
- Create, delete, and assign admins and net control operators (NCOs) to talkgroups
- List, suspend, approve, or edit users
- View last heard for a specific user or repeater

## The Navigation Bar

When logged in as an administrator, the navigation bar displays an **Admin** dropdown with links to:

- **Talkgroups** — manage all talkgroups
- **Repeaters** — manage all repeaters
- **Users** — manage user accounts
- **User Approvals** — process pending registrations
- **Messages**: search all stored text and data messages (see [Messages](#messages))
- **Setup** — update configuration settings

## Talkgroups

The admin talkgroup page is similar to the main talkgroup list, but all talkgroups' names, descriptions, admins, and NCOs are editable and all talkgroups can be deleted.

### Creating Talkgroups

Click **Add New Talkgroup** in the table header. Name and Description are required; Talkgroup Administrators and NCOs are optional.

## Repeaters

The admin repeaters page lists all repeaters on the system, not just those owned by the current user.

With several replicas, a repeater that moves to another replica in a rolling restart keeps its login. The load balancer can drop what the old replica sent just before the move, so the old replica hands that over and the new replica sends it again. For an IPSC peer or a browser hotspot, this is what went out in the last 500 ms, and the peer drops what it already got. MMDVMHost and DMRGateway drop a repeated frame only when it repeats the last frame, and play other repeated frames as audio. So for an MMDVM repeater this is only the last frame on each timeslot that went out in the last 60 ms. When the load balancer takes more than 60 ms to pass on the packet that moves an MMDVM repeater, the repeater loses the bursts that the old replica sent before that. After a stall, frames can go out closer together than 60 ms, and the repeater can lose all but the last of them.

## Users

The users page allows administrators to edit and delete users. Current editable actions include suspending and unsuspending users.

## User Approvals

This list shows users who have registered and are awaiting access. These users have already been validated as having a valid DMR ID and callsign combination.

> **Note:** RadioID.net validation confirms the callsign/DMR ID pair exists in the database, but does not verify the registrant actually owns that callsign. Use your judgment when approving users.

Click **Approve** to grant the user access to sign in and use DMRHub.

## Setup

The setup page allows administrators to edit the DMRHub configuration. All configuration options are available here. In deployments with read-only file-based configuration (e.g., Docker), this page is view-only and shows the current effective configuration.

## OpenBridge peers

When OpenBridge is on, the **Peers** page in the admin menu lists the peers, and **New Peer** adds one. Each peer has a network ID, a passphrase, an address and port, and switches to receive traffic from it and to send traffic to it.

Turn on **Enhanced** for a FreeDMR server whose link to this hub has `ENHANCED_OBP: True`. Only an admin can change it. For an enhanced peer, DMRHub does what FreeDMR's `hblink.py` does on such a link:

- It sends DMRE version 5 packets in place of the 73-octet DMRD packets. Each packet carries the send time, the source server, the source repeater and the hop count.
- It keeps the source server and the source repeater of a call that came in from another enhanced peer, and adds one to the hop count.
- It drops a call that already crossed 20 links (FreeDMR's `MAX_FBP_HOPS`) and asks the sender with a BCSQ to stop that stream.
- It sends a BCKA keepalive every 10 seconds. FreeDMR stops sending to a peer that sent no keepalive for 60 seconds.

The source server of a call from a local repeater is the **Server ID** in the OpenBridge settings, or the network ID of the peer when the server ID is 0. FreeDMR wants a source server of 4 to 7 digits, and it checks a 4 or 5-digit one against its list of server IDs. Other peers keep getting OpenBridge version 1.

With several replicas, one replica sends all OpenBridge traffic, so that peers see one source address. It holds a lease in Redis. When it stops, another replica takes over at once, and when it dies, within 2 seconds. Traffic in that gap is dropped, not delayed. A BCSQ from a peer applies on every replica, whichever replica got it.

## Transmission kinds

Each Last Heard entry has a `kind` and a `sub_kind`. The API and the websocket give them in these fields, and the values stay the same between releases. A sub kind that names a CSBK opcode, a SAP, a port, a USBD service type or a text format is the dmrgo name of that value in lower case, without "PDU", with `_` between the words. For example, the BS outbound activation CSBK is `bs_outbound_activation_or_td_grant_mi`.

| `kind`    | `sub_kind`                                                                                                                                                                             |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `voice`   | empty, or `encrypted` when the call had a PI header                                                                                                                                    |
| `data`    | `sms` for a text message, `response` for an ACK, NACK or SACK, the port name of an IP packet (`lrrp`, `ars`, `tms`, `text`, `location` and others), `ip` for another IP packet, or the SAP name (`short_data`, `proprietary_packet_data` and others) |
| `udt`     | empty                                                                                                                                                                                  |
| `csbk`    | `preamble` when no data followed it, `radio_check`, `radio_check_answer`, `call_alert`, `call_alert_ack`, another standard opcode name, or a Capacity Plus opcode name such as `capacity_plus_site_status` |
| `mbc`     | the opcode name, such as `c_bcast`                                                                                                                                                    |
| `usbd`    | the service type name, such as `location_information_protocol`                                                                                                                       |
| `idle`    | empty                                                                                                                                                                                  |
| `alias`   | empty, or `partial` when only part of the alias arrived                                                                                                                                |
| `gps`     | empty, or `partial` for a short DMRG that has no coordinates                                                                                                                          |
| `unknown` | `bad_crc`, `undecodable`, `reserved`, `frame_type_3`, or `csbk` for a CSBK opcode that DMRHub does not know                                                                         |

The `data_format` of a text message is `tms`, `ip`, `anytone`, `defined_data`, `hytera` or `unknown`.

The `undeliverable` list has one sentence for each target that could not take a burst of the entry. Each sentence names the target and the reason, for example `IPSC repeater 314926 was busy on that slot.`. The `reason` label of the `dmr_undeliverable_bursts_total` metric keeps the reason codes: `invalid_data_type`, `no_ipsc_form`, `frame_type_3`, `fec_uncorrectable`, `slot_type`, `stream_id_range`, `build_failed`, `encode_failed`, `data_in_voice`, `undecodable` and `slot_busy`.

## Messages

DMRHub routes text messages (SMS) and other data messages between radios. For what users see, see [Messages and SMS Storage](messages.md).

### What the hub stores

DMRHub decodes and stores every text message and data message that it routes. It does this for all users, whatever their settings. The hub owner is responsible for the traffic that the hub routes, and uses these records to make sure that the traffic obeys the amateur radio rules.

For each message, DMRHub stores:

- the time, the source radio ID, and the destination radio ID or talkgroup;
- the repeater, the timeslot, and the network (MMDVM, IPSC or OpenBridge) that the message came from;
- the format and the text, when the message is text;
- if the message was confirmed, and the reply (ACK, NACK or SACK);
- the raw bytes: the data header, then the reassembled user data.

DMRHub reads these text formats:

| Format          | Description                                                  |
| --------------- | ------------------------------------------------------------ |
| ETSI IP         | Text in an IP and UDP packet                                 |
| Motorola TMS    | Motorola Text Messaging Service                              |
| AnyTone         | AnyTone text messages                                        |
| Hytera          | Hytera text messages                                         |
| Defined Data    | Text sent as ETSI defined short data                         |
| Data (not text) | A data message that is not text, for example location data   |

### Search all messages

Admins can search every stored message, in every format, from every user. Go to **Admin**, then **Messages** (`<siteurl>/admin/messages`). The search has no 250-message limit and does not use the SMS Storage setting of the users.

You can search by:

- **From ID**: the source radio ID;
- **To ID or talkgroup**: the destination radio ID or talkgroup;
- **Format**;
- **From date** and **To date**: the days in your local time zone, both included;
- **Text contains**: a part of the text, in upper or lower case.

Click a row to see the details of the message and its raw bytes.

### What users can see

A user sees a message only when all of these are true:

- SMS Storage is on for the user.
- The message arrived after the user last turned on SMS Storage. When a user turns SMS Storage off and on again, the start time moves to the new time.
- The message is to (inbox) or from (outbox) a radio ID that the user owns. For an added ID, the message also arrived after DMRHub verified the ID.
- The message is one of the 250 newest of its box.

The counts and statistics on the Messages page use the same rules, so they never show more than 250 messages in a box, or messages from before the start time.

### Retention

By default, DMRHub keeps all records forever. To delete old records, go to **Admin**, then **Setup**, and find the **Retention** card in **DMR Settings**. Set each of these:

| Setting       | What it deletes                                          | Configuration key           |
| ------------- | -------------------------------------------------------- | --------------------------- |
| Messages      | Text and data messages, with their raw bytes             | `dmr.retention.messages`    |
| Transmissions | Last heard entries for voice, data and control traffic   | `dmr.retention.transmissions` |
| Raw bursts    | The raw DMR bursts of each transmission                  | `dmr.retention.bursts`      |

Each value is a number of days, from 0 to 36500. 0 keeps the records forever. The Setup page offers Keep forever, 30 days, 90 days, 1 year, 2 years and 5 years. A different value from the configuration file shows as a number of days.

Retention also deletes records from the admin search. Set a period that your local rules allow.

## Plugins

Plugins answer the calls to the IDs you give them, such as the parrot at 9990 and the location server at 5050 to 5059. DMRHub runs the parrot, the location server and the plugins you upload inside itself, by default. See [Plugins](plugins.md) to upload plugins, and to run plugins as their own programs.

## Positions

The location server is a plugin: see [Plugins](plugins.md#location-server). For what users see, see [Positions](positions.md).

### What the hub stores

DMRHub stores positions only for the radios whose owners turn on position tracking, and only from that time. It stores at most one position of a radio each 30 seconds: the latitude and longitude, the accuracy, speed, heading and altitude when the radio sends them, the time of the fix and of its arrival, the repeater and timeslot, and the location service ID. When the owner turns tracking off, DMRHub deletes the positions of the radio.

Admins see the same positions as everyone else: their own radios, and the radios of the users who share when they share too. Apps never get admin rights.

### Limits

Go to **Admin**, then **Setup**, and find the **Positions** card in **DMR Settings**.

| Setting                         | What it does                                                                 | Configuration key              |
| ------------------------------- | ---------------------------------------------------------------------------- | ------------------------------ |
| Shortest report interval        | Owners can pick 5, 10, 15, 30 or 60 minutes, but not less than this.         | `dmr.positions.min-interval`   |
| Longest time to keep positions  | Owners can keep positions for 24 hours, 7 days, 30 days or this many days, from 1 to 365. | `dmr.positions.max-retention`  |

Each hour, DMRHub deletes the positions older than the time the owner picked, and never keeps them longer than the longest time.

### APRS-IS

DMRHub logs in to APRS-IS once, and sends the positions of the radios whose owners turn it on, each under its owner's callsign. Set the login on the same card:

| Setting  | What it is                                                     | Configuration key                |
| -------- | -------------------------------------------------------------- | -------------------------------- |
| Callsign | The callsign and SSID that the hub logs in as, such as `N0HUB-10`. Empty sends nothing. | `dmr.positions.aprs-is.callsign` |
| Server   | The APRS-IS server, as host and port                           | `dmr.positions.aprs-is.server`   |

DMRHub works out the APRS-IS passcode from the callsign, so there is nothing to register. Use a callsign that you hold a license for. The server is `rotate.aprs2.net:14580` by default. When DMRHub runs as several replicas, only one of them logs in at a time.

Each packet has the destination `APZDMH` and the path `<repeater or hotspot callsign>*,qAR,<hub login>`, like the packets of the BrandMeister APRS gateway.

### The map

The Positions page loads its map from `https://tiles.squallar.app`, without cookies, and keeps the map data it has read in the browser's cache. If you add a Content-Security-Policy, allow `connect-src https://tiles.squallar.app`, `worker-src 'self' blob:`, `img-src 'self' data: blob:` and `style-src 'unsafe-inline'`.
