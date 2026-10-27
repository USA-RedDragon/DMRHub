# Link a repeater

This guide tells you how to link a repeater to DMRHub. For a hotspot that uses your own DMR ID, see [Connect a hotspot](connect-hotspot.md).

## Repeater IDs

A repeater uses one of these IDs:

- A 6-digit repeater ID from [RadioID.net](https://radioid.net/). RadioID.net must list the repeater under your callsign. DMRHub copies the color code, frequencies, location and other details of the repeater from the RadioID.net database.
- Your 7-digit DMR ID, or your DMR ID with two more digits, as for a hotspot.

| Message | Meaning |
| --- | --- |
| Repeater ID is not valid | RadioID.net does not list this 6-digit repeater ID. |
| Repeater ID does not match assigned callsign | RadioID.net lists the repeater under another callsign. |
| Hotspot ID must start with user ID | The 7 or 9-digit ID does not start with the DMR ID of your account. |
| RadioID is invalid | The ID does not have 6, 7 or 9 digits. |
| IPSC is not enabled on this server | You picked Motorola IPSC, but the network does not accept it. |

## MMDVM repeaters

A repeater that runs MMDVMHost, with or without DMRGateway, connects like a hotspot:

1. Go to **Repeaters** and click **Enroll New Repeater**.
2. Set **Repeater Type** to **MMDVM**, if the page shows the field.
3. Enter the repeater ID in **DMR Radio ID** and click **Save**.
4. DMRHub opens the connect page of the repeater. Copy the password now. DMRHub shows it only once.
5. Pick the tab of your software on the connect page and follow its steps. The values of the repeater are already filled in. See [Connect a hotspot](connect-hotspot.md#set-up-your-software).

If you lose the password, open the page of the repeater, click **Connect**, then click **Rotate password**. See [Rotate the password](connect-hotspot.md#rotate-the-password).

An older password can have a `#` or a `%`, which some repeater software does not read correctly. If your password has one, rotate it the same way.

### Repeaters that use the published Homebrew specification

Some repeater software follows the published Homebrew repeater specification instead of the MMDVM network protocol. The two protocols have different login and keepalive messages. Enroll such a repeater as **MMDVM** and use the same address, port and password. DMRHub detects the protocol at login, and the **Protocol** field on the page of the repeater shows which one it used.

This protocol has no options message, so the repeater cannot send its static talkgroups. Set them on the Repeaters page instead. It also has no talker alias or GPS messages.

## Motorola IPSC repeaters

DMRHub accepts Motorola repeaters (for example the SLR 5500 or XPR 8400) over IPSC when the admin turned on IPSC. If the network does not accept IPSC, the **Repeater Type** field does not show.

1. Go to **Repeaters** and click **Enroll New Repeater**.
2. Set **Repeater Type** to **Motorola IPSC**.
3. Enter the repeater ID and click **Save**.
4. DMRHub opens the connect page of the repeater. Copy the auth key now. DMRHub shows it only once.
5. In the CPS of the repeater, set up an IPSC network link with the values on the **Motorola CPS** tab of the connect page:
   - **Radio ID**: the repeater ID.
   - **Link Type**: Peer.
   - **Master IP**: the server address on the connect page.
   - **Master UDP Port**: the port on the connect page (50000 by default).
   - **Authentication Key**: the 40-digit hex key.

If you lose the auth key, open the page of the repeater, click **Connect**, then click **Rotate auth key** and confirm. The old key stops working at once, and the repeater cannot link until you put the new key in the CPS and write the codeplug.

For CPS details, see the [BrandMeister IPSC wiki](https://wiki.brandmeister.network/index.php/IPSC).

> **Note:** The repeater connects to the address of DMRHub. Ask your admin whether the address of the network stays the same. An IPSC link to a server whose address changes stops working until you change the CPS.

### What an IPSC repeater transmits

DMRHub sends voice, CSBK, data headers, and rate 1/2 and 3/4 data to IPSC repeaters in the IPSC form. IPSC has no form for rate 1 data, so DMRHub sends rate 1 data as raw blocks. A test with an XPR 8400 showed that it transmits these raw blocks bit for bit on both timeslots.

The same test showed that an XPR 8400 drops these bursts when it gets them from the network, and gives no error:

- idle bursts, USBD and reserved data types
- a CSBK or a data header with a bad CRC
- a block whose FEC does not correct
- rate 1/2 data blocks without their data header
- an MBC without its last block
- terminator LC (TD_LC)

DMRHub does not send idle bursts, USBD, reserved data types or bursts whose FEC does not correct to IPSC repeaters. It marks each of them on the Last Heard row of the transmission. The caller and an admin see every repeater that the transmission did not go to. The owner of a repeater sees only their own repeater. Other people see nothing. DMRHub sends the other bursts in the list, and the XPR 8400 drops them. DMRHub counts and marks a data burst in a voice stream, and a burst that it cannot put in an IPSC packet, in the same way.

## After the repeater connects

- [Pick the talkgroups of the repeater](talkgroups.md)
- [Set the voice codec of each timeslot](voice-codec.md)
