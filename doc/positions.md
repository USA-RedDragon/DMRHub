# Positions

DMRHub can be the location server of your radios. When position tracking is on for a radio, DMRHub asks the radio for its position, keeps the reports it sends, and shows them on a map. It can also send the positions to APRS-IS.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

## Set up the radio

DMRHub answers as the location server on the IDs 5050 to 5059. Program your radio to send its registrations (ARS) and its location reports to one of these IDs. All ten work the same way. The ID only picks the symbol that APRS-IS shows when you do not pick one yourself:

| ID   | Symbol  | ID   | Symbol  |
| ---- | ------- | ---- | ------- |
| 5050 | House   | 5055 | Phone   |
| 5051 | Train   | 5056 | Camp    |
| 5052 | Tractor | 5057 | Person  |
| 5053 | Pickup  | 5058 | Bike    |
| 5054 | Van     | 5059 | Car     |

DMRHub reads LRRP, NMEA and Hytera (HDAP) location reports, and answers the confirmed ones.

## Turn on position tracking

Position tracking is off for every radio by default. To turn it on:

1. Log in.
2. Go to `<siteurl>/positions` (**Positions** in the navigation bar).
3. Click **Settings** next to the radio ID.
4. Turn on **Position tracking**, pick how often the radio reports and how long to keep its positions, then click **Save**.

The next time the radio registers, DMRHub asks it to report at that interval. DMRHub keeps the request when it restarts. It asks again when the radio registers again, and once more when the radio is heard but has not reported for three intervals.

DMRHub stores at most one position of a radio each 30 seconds, and only positions that arrived after you turned tracking on. The hub sets the shortest interval and the longest time that you can pick.

When you turn tracking off, DMRHub tells the radio to stop reporting and deletes the positions it kept for that radio.

Your account ID and the radio IDs you added on the Settings page are listed. See [Radio IDs](messages.md#radio-ids).

## The Positions page

The map shows where each radio last reported from and its track. Pick **24 hours**, **7 days** or **30 days** to see a longer track. New positions appear while the page is open. Click a radio, on the map or in the list, to see its last position, its accuracy, and the repeater and timeslot it came through.

On the settings page of a radio, **Download the track** saves its positions as GPX, GeoJSON or CSV.

## Sharing

Only you see the positions of your radios. Turn on **Share my positions** on the Positions page to see the radios of the other users who share theirs. They then see yours. Admins see the same as everyone else.

## APRS-IS

When the hub has an APRS-IS login, you can send the positions of a tracked radio to APRS-IS, where sites such as aprs.fi show them. APRS-IS shows positions to anyone.

On the settings page of the radio, turn on **Send to APRS-IS**, then pick:

- **SSID**: added to your callsign, for example `-9` for a mobile or `-7` for a handheld. Pick none to send your callsign alone.
- **Symbol**: the symbol on the map. **As the radio reports it** uses the symbol of the location service ID in the table above.
- **Comment**: up to 43 characters, shown with the position. DMRHub shortens it when the speed or altitude takes up room.

DMRHub sends a position under your callsign, through the repeater or hotspot that heard it. It sends at most one position of a radio each minute, and does not send a position again when the radio has not moved more than its accuracy. It does not send positions that are older than two minutes, that have no fix, or that are not on Earth. Your callsign must be a licensed amateur callsign.

## For apps

An app that you allow with the `positions:read` scope can read the positions that you see:

| Request                                         | Answer                                                                 |
| ----------------------------------------------- | ---------------------------------------------------------------------- |
| `GET /api/v1/positions?hours=24`                | The newest position of each radio you see. `hours` is 24, 168 or 720.  |
| `GET /api/v1/positions/:id?hours=24`            | The newest position and the track of one radio.                        |
| `GET /api/v1/positions/:id/track?format=gpx`    | The track of your own radio as `gpx`, `geojson` or `csv`, between the RFC 3339 times `from` and `to`. |

The websocket `/ws/messages` sends each new position as `{"kind":"position","position":{...}}`. An app with `positions:read` gets only positions on it, and an app with `messages:read` gets only messages.
