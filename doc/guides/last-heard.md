# See who is on the air

The **Last Heard** page (`<siteurl>/lastheard`) lists the recent transmissions on the network.

<!-- screenshot: lastheard.png | the Last Heard page at /lastheard, logged in, with voice and SMS rows -->

## What the list shows

- When the page loads, it lists the latest group transmissions to talkgroups, newest first. Anyone can see them.
- Visitors who are not logged in see no text messages.
- When you are logged in, new transmissions appear while the page is open: your own, private ones to you or to your hotspots, and the talkgroups that your hotspots carry. Logged-in users also see the **Repeater** column.

Use **Type** to pick which kinds of transmission to show: **Voice**, **Data**, **SMS**, **GPS**, **Talker Alias**, **CSBK**, **Control** and **Unknown**. By default the list shows every kind but **Unknown**. **All** turns on every kind and **Show idle and USBD rows**.

## Columns

| Column | Meaning |
| --- | --- |
| Time | When the transmission started |
| TS | The timeslot |
| Type | The kind of transmission |
| User | The radio that transmitted |
| Destination | The talkgroup or radio that it went to |
| Repeater | The hotspot or repeater that it came through (logged-in users only) |
| Duration | How long it lasted |
| Codec | AMBE or Codec2, and whether DMRHub converted the call (see [Use Codec2 radios](voice-codec.md)) |
| BER | Bit error rate of a voice call, if the hotspot or repeater sends it |
| Loss | Percent of voice packets lost |
| Jitter | A running average of how far the packet interval was from 60 ms. Positive means that packets came slower than every 60 ms, negative means faster. |
| RSSI | Signal strength, if the hotspot or repeater sends it |

BER, Loss, Jitter and RSSI show only for voice calls.

## Last heard of one hotspot or talkgroup

- Click the ID of a hotspot on the Repeaters page to see the calls through it. Only its owner and admins see this.
- Click a talkgroup on the Talkgroups page to see the calls on it.
