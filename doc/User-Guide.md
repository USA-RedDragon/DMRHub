# User's Guide

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

## Theming

DMRHub ships with Material Dark Compact Indigo as the default theme. Click the palette icon on the right side of the page to open the theming panel and choose your preferred look and feel. Your selection is saved to the browser's local storage.

## Home Page

The home page contains general information about the DMRHub instance and useful links.

## Last Heard

The last heard page shows recent DMR call activity:

- **Not logged in** — shows previously seen public talkgroup calls
- **Logged in** — shows calls involving your DMR user ID, your repeater IDs, or talkgroups you're subscribed to

The list updates in real-time. The table includes:

| Column |                                                      Description                                                      |
| ------ | --------------------------------------------------------------------------------------------------------------------- |
| RSSI   | Signal strength (if the source repeater sends it)                                                                     |
| BER    | Bit Error Rate (if the source repeater sends it)                                                                      |
| Jitter | Average timing deviation from the 60ms DMR packet interval. Positive = packets too slow, negative = packets too fast. |

## Messages and Settings

When you are logged in, the **Messages** page shows the text messages sent to and from your radio IDs, and the **Settings** page lets you turn on SMS Storage and add the other radio IDs that you own. See [Messages and SMS Storage](messages.md).

## Registration

Visit `<siteurl>/register` to create an account. You'll need:

- Your [RadioID.net](https://radioid.net/) DMR ID
- A username
- Your callsign
- A password

DMRHub validates that the DMR ID and callsign match in the RadioID.net database. The user database is updated automatically at startup and daily at midnight UTC.

If the instance has [Have I Been Pwned](https://haveibeenpwned.com/) checking enabled, previously breached passwords will be rejected. Passwords are immediately salted and hashed with Argon2i upon registration and never stored in plain text.

> **Security note:** On networks like AREDN that use plaintext communications, your password could be observed in transit. Always use a unique password for DMRHub.

After registration, an administrator must approve your account before you can log in.

## Enrolling a Repeater or Hotspot

DMRHub treats hotspots and repeaters identically. Supported repeater IDs:

- A **6-digit** repeater ID issued by RadioID.net — the registered owner must match your account
- Your **7-digit** DMR radio ID from RadioID.net
- Your **9-digit** DMR radio ID suffixed with a two-digit number (00–99)

To register a repeater:

1. Navigate to `<siteurl>/repeaters/new`
2. Select the repeater type: **MMDVM** or **Motorola IPSC**
3. Enter your repeater ID
4. You'll be shown connection configuration details for your repeater type

> **Important:** The generated password/auth key is shown only once. Make sure to save it.

### MMDVM Repeaters

For MMDVM-based repeaters, you'll receive a [DMRGateway](https://github.com/g4klx/DMRGateway) configuration snippet with the address, port, and password needed to connect.

### Motorola IPSC Repeaters

For Motorola DMR repeaters (DR3000, SLR5500, etc.), you'll receive an IPSC HMAC-SHA1 auth key. Configure your repeater's IPSC network settings:

- **Master IP:** The IP of your DMRHub instance
- **Master Port:** 50000 (default)
- **Auth Key:** The hex key shown after repeater creation

For detailed Motorola CPS configuration, see the [BrandMeister IPSC wiki](https://wiki.brandmeister.network/index.php/IPSC).

> **Note:** IPSC connections require the DMRHub instance to have a static IP address. The official DMRHub instance uses a dynamic IP and cannot reliably support IPSC connections.

#### What an IPSC repeater transmits

DMRHub sends voice, CSBK, data headers and rate 1/2 and 3/4 data to IPSC repeaters in the IPSC form. IPSC has no form for rate 1 data, so DMRHub sends rate 1 data as raw blocks. A test with an XPR 8400 showed that it transmits these raw blocks bit for bit on both timeslots.

The same test showed that an XPR 8400 does not transmit these bursts when it gets them from the network. It drops them and gives no error:

- idle bursts, USBD and reserved data types;
- a CSBK or a data header with a bad CRC;
- a block whose FEC does not correct;
- rate 1/2 data blocks without their data header;
- an MBC without its last block;
- terminator LC (TD_LC).

DMRHub does not send idle bursts, USBD, reserved data types or bursts whose FEC does not correct to IPSC repeaters. It counts each of them in the `dmr_undeliverable_bursts_total` metric and marks it on the Last Heard row of the transmission. DMRHub sends the other bursts in the list, and the XPR 8400 drops them.

## Repeaters Page

Once your repeater is registered and connecting, the repeaters page shows:

- Last connected time
- Last ping time (updated every 5 seconds)

Click a repeater's icon to edit its dynamic and static talkgroup assignments. The dropdown boxes are searchable by talkgroup name and ID. If a talkgroup is deleted while you edit, saving fails with "talkgroup N does not exist" and nothing changes.

## Talkgroups

The talkgroup list is available at `<siteurl>/talkgroups`. These are managed by DMRHub administrators.

Parrot (TG 9990) is listed as a talkgroup, but it always responds with a private call and does not route traffic like standard talkgroups.

### Linking and Unlinking

- **Static talkgroups** can be configured from the Repeaters page, or sent by the repeater or hotspot itself (see below)
- **Dynamic linking** happens automatically when you transmit on a talkgroup — it links on the same timeslot
- **Unlinking** by transmitting on TG 4000 on the timeslot you want to unlink

### Static Talkgroups from the Repeater (Options)

A repeater or hotspot can send its static talkgroups when it connects, in the `Options` line of the `[DMR Network]` section of MMDVMHost or DMRGateway. DMRHub reads the same keys as FreeDMR, and the DMR+ keys too:

- `TS1=91,3100;TS2=9;` (or `TS1_STATIC=` and `TS2_STATIC=`) set the static talkgroups of each timeslot.
- `TS1_1=91;TS1_2=3100;TS2_1=9;` (the DMR+ form) do the same, with up to nine talkgroups per timeslot.

As in FreeDMR, options with static talkgroups set both timeslots. They replace the static talkgroups on the Repeaters page, take effect at once, and are saved, so the Repeaters page shows what is active. If you change them on the Repeaters page later, the repeater puts its own back the next time it connects.

- A timeslot that the options do not mention loses its static talkgroups. `TS2=9;` alone leaves TS1 with none.
- An empty list, such as `TS2=;`, also removes all static talkgroups of that timeslot.
- Talkgroups that DMRHub does not have are left out. If none of the talkgroups in a list exist, or the list has something that is not a talkgroup number, DMRHub leaves that timeslot as it is.
- Options with no static talkgroups at all, such as `DIAL=0;`, leave the static talkgroups as they are.

The default dial of a timeslot links a talkgroup as that timeslot's dynamic talkgroup, just as transmitting on it would. Like any other dynamic link, it changes when someone transmits on another talkgroup, and TG 4000 unlinks it.

- `DIAL=91;` (or `DIAL2=`, `DEFAULT_REFLECTOR=`, `default_dial_ts2=`, or the DMR+ `StartRef=`) links TG 91 on TS2. `DIAL1=` (or `default_dial_ts1=`) does the same on TS1.
- `DIAL=0;` or an empty value unlinks the dynamic talkgroup of that timeslot. So do the values FreeDMR does not take as a default dial: 1 to 9 and 9990 to 9999.
- A talkgroup that DMRHub does not have is ignored.
- Without a default dial, the dynamic talkgroup stays as it is.

The repeater sends its options each time it connects, so the default dial is linked again at every login.

DMRHub has no timers, voice announcements or single mode, so it ignores the other FreeDMR and DMR+ options, such as `TIMER`, `RelinkTime`, `VOICE` and `SINGLE`. The DMRHub log lists what it applied and what it ignored.

### Talkgroup Administration

Talkgroup admins can edit the talkgroup name and description, and appoint net control operators (NCOs).
