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

## Repeaters Page

Once your repeater is registered and connecting, the repeaters page shows:

- Last connected time
- Last ping time (updated every 5 seconds)

Click a repeater's icon to edit its dynamic and static talkgroup assignments. The dropdown boxes are searchable by talkgroup name and ID.

## Talkgroups

The talkgroup list is available at `<siteurl>/talkgroups`. These are managed by DMRHub administrators.

Parrot (TG 9990) is listed as a talkgroup, but it always responds with a private call and does not route traffic like standard talkgroups.

### Linking and Unlinking

- **Static talkgroups** can be configured from the Repeaters page
- **Dynamic linking** happens automatically when you transmit on a talkgroup — it links on the same timeslot
- **Unlinking** by transmitting on TG 4000 on the timeslot you want to unlink

### Talkgroup Administration

Talkgroup admins can edit the talkgroup name and description, and appoint net control operators (NCOs).
