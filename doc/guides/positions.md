# Share the position of your radio

DMRHub can track where your radio is, show it on a map, share it with the other users of the network, and send it to APRS-IS. Your radio must have GPS. DMRHub gets the position from location reports to the location server plugin, and from the GPS that the radio sends in a voice call when the hotspot or repeater passes it on. See [Where positions come from](../positions.md#where-positions-come-from).

For the facts behind each setting, see the [Positions reference](../positions.md).

## Step 1: Set up the radio

1. In the radio programming software, turn on GPS.
2. Set the radio to send its ARS registration and its location reports to one of the location server IDs, usually 5050 to 5059. Ask your admin for the IDs of your network. All ten IDs work the same way. The ID only picks the default APRS symbol (see the [table of symbols](../positions.md#location-server-ids)).
3. Turn on ARS, so that the radio registers when it turns on.

If the radio sends its GPS in voice calls instead, steps 2 and 3 are not necessary. DMRHub then gets a position each time you talk through a hotspot or repeater that passes the GPS on.

## Step 2: Turn on position tracking

Position tracking is off for each radio until you turn it on.

1. Log in and go to **Positions** (`<siteurl>/positions`).
2. Find the radio ID in the **Radios** list and click **Settings**.
3. Turn on **Position tracking**.
4. Pick **Report every** (how often the radio reports) and **Keep positions for** (how long DMRHub keeps them).
5. Click **Save**.

<!-- screenshot: radio-positions-settings.png | the settings page of one radio at /positions/3000001 with tracking on, 10 minutes and 7 days -->

The next time the radio registers or DMRHub hears it, the location server asks the radio to report at that interval.

The list has your account ID and the radio IDs that you added on the Settings page. To add a radio, see [Add your other radio IDs](text-messages.md#add-your-other-radio-ids).

## Step 3: See the map

On the **Positions** page, the map shows where each radio last reported from, and its track. Pick **24 hours**, **7 days** or **30 days** to see a longer track. Click a radio, on the map or in the list, to see its last position, its accuracy, and the repeater and timeslot it came through.

<!-- screenshot: positions-map.png | the Positions page at /positions with one radio and its track for 24 hours -->

## Share your positions with other users

Only you see the positions of your radios, until you share them.

1. Go to **Positions**.
2. Turn on **Share my positions**.

Every logged-in user then sees your radios on their map. You see the radios of other users who share theirs, whether or not you share yours.

## Send your positions to APRS-IS

When the network has an APRS-IS login, you can send the positions of a tracked radio to APRS-IS. Sites such as aprs.fi then show them. Anyone can see positions on APRS-IS.

1. Open the settings page of the radio (**Positions**, then **Settings** next to the radio).
2. In the **APRS-IS** card, turn on **Send to APRS-IS**.
3. Pick the **SSID**, the **Symbol** and an optional **Comment**.
4. Click **Save**.

Your callsign must be a licensed amateur callsign.

## Download a track

On the settings page of a radio, use **Download the track** to save its positions as GPX, GeoJSON or CSV.

## Turn off tracking

On the settings page of the radio, turn off **Position tracking** and click **Save**. DMRHub tells the radio to stop reporting and deletes the positions it kept for that radio.
