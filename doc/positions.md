# Positions reference

This page lists the facts about position tracking on DMRHub. For the steps to set it up, see [Share the position of your radio](guides/positions.md).

## Location server IDs

The location server is a plugin. It answers on the IDs that your admin gives it, usually 5050 to 5059. All ten IDs work the same way. The ID only picks the symbol that APRS-IS shows when you leave **Symbol** at **As the radio reports it**:

| ID   | Symbol  | ID   | Symbol  |
| ---- | ------- | ---- | ------- |
| 5050 | House   | 5055 | Phone   |
| 5051 | Train   | 5056 | Camp    |
| 5052 | Tractor | 5057 | Person  |
| 5053 | Pickup  | 5058 | Bike    |
| 5054 | Van     | 5059 | Car     |

The location server reads LRRP, NMEA and Hytera (HDAP) location reports, and answers the confirmed ones.

## Where positions come from

| Source | Format on the Positions page |
| --- | --- |
| A location report to a location server ID | LRRP, NMEA or HDAP |
| The GPS that a radio sends in a voice call, heard by the modem of DMRHub | GPS |
| The GPS that a hotspot or repeater sends to DMRHub as a `DMRG` packet, for example from MMDVMHost | GPS |

All sources follow the same rules: tracking must be on, and DMRHub keeps at most one position of a radio each 30 seconds from all sources together. A GPS position has no speed, heading, altitude or fix time. Its accuracy is the position error that the radio sent, when the radio knows it.

## Which radios you can track

The **Radios** list on the Positions page has your account ID and the radio IDs that you added on the Settings page. Only the owner of a radio ID can change its tracking.

## When the radio reports

- Position tracking is off for each radio until its owner turns it on.
- When tracking is on, the location server asks the radio to report at the interval you picked. It asks when the radio registers, and again when the radio is heard but has not reported for three intervals.
- When you change the interval, or turn tracking off, the location server tells the radio.

## Intervals and retention

| Setting | Choices |
| --- | --- |
| Report every | 5, 10, 15, 30 and 60 minutes, starting at the shortest interval that the admin allows (5 minutes by default). |
| Keep positions for | 1 day, 7 days and 30 days, each when it is not longer than the longest time that the admin allows, and that longest time (30 days by default, at most 365 days). |

## What DMRHub stores

- At most one position of a radio each 30 seconds, from all sources together.
- Only positions that arrived after you turned tracking on.
- Each position for the retention time that you picked. Then DMRHub deletes it.
- When you turn tracking off, DMRHub deletes all positions of that radio.

## Who sees a position

| Viewer | Sees |
| --- | --- |
| You | The radios that you track. |
| Other logged-in users | Your radios, when you turn on **Share my positions**. |
| Admins | The same as any other user. |
| Visitors who are not logged in | Nothing. |
| Anyone on APRS-IS | The positions of a radio with **Send to APRS-IS** on. |

You see the shared radios of other users whether or not you share your own.

## APRS-IS

| Setting | Meaning |
| --- | --- |
| SSID | Added to your callsign, for example `-9` for a mobile or `-7` for a handheld. Pick none to send your callsign alone. |
| Symbol | The symbol on the map. **As the radio reports it** uses the symbol of the location server ID. |
| Comment | Up to 43 characters, shown with the position. DMRHub shortens it when the speed or altitude takes up room. |

DMRHub sends a position under your callsign, through the hotspot or repeater that heard it. It sends at most one position of a radio each minute, and sends a position again only when the radio moved more than its accuracy. It does not send positions that are older than two minutes, that have no fix, or that are not on Earth. Your callsign must be a licensed amateur callsign. The network must have an APRS-IS login.

## Track downloads

**Download the track** on the settings page of a radio saves its positions as GPX, GeoJSON or CSV.

## For apps

An app that you allow with the `positions:read` scope can read the positions that you see:

| Request | Answer |
| --- | --- |
| `GET /api/v1/positions?hours=24` | The newest position of each radio you see. `hours` is 24, 168 or 720. |
| `GET /api/v1/positions/:id?hours=24` | The newest position and the track of one radio. |
| `GET /api/v1/positions/:id/track?format=gpx` | The track of your own radio as `gpx`, `geojson` or `csv`, between the RFC 3339 times `from` and `to`. |

The websocket `/ws/messages` sends each new position as `{"kind":"position","position":{...}}`. An app with `positions:read` gets only positions on it, and an app with `messages:read` gets only messages.
