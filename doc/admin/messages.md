# Messages and retention

DMRHub routes text messages (SMS) and other data messages between radios. This page tells what the hub stores, how an admin searches it, and how long DMRHub keeps it. For what users see, see the [Messages reference](../messages.md).

## What the hub stores

DMRHub decodes and stores every text message and data message that it routes. It does this for all users, whatever their settings. The hub owner is responsible for the traffic that the hub routes, and can use these records to check that the traffic obeys the amateur radio rules.

For each message, DMRHub stores:

- The time, the source radio ID, and the destination radio ID or talkgroup.
- The repeater, the timeslot, and the network (MMDVM, IPSC or OpenBridge) that the message came from.
- The format and the text, when the message is text.
- Whether the message was confirmed, and the reply (ACK, NACK or SACK).
- The raw bytes: the data header, then the reassembled user data.

## Search all messages

Admins can search every stored message, in every format, from every user. Go to **Admin**, then **Messages** (`<siteurl>/admin/messages`). The search has no 250-message limit, and does not use the SMS Storage setting of the users.

You can search by:

- **From ID**: the source radio ID.
- **To ID or talkgroup**: the destination radio ID or talkgroup.
- **Format**.
- **From date** and **To date**: the days in your local time zone, both included.
- **Text contains**: a part of the text, in upper or lower case.

Click a row to see the details of the message and its raw bytes.

<!-- screenshot: admin-messages.png | /admin/messages with a search for From ID 3000001 and one result row open, showing the details and raw bytes -->

## Retention

By default, DMRHub keeps all records forever. To delete old records:

1. Go to **Admin**, then **Setup**, and find the **Retention** card in **DMR Settings**.
2. Pick a period for each kind of record:

   | Setting | What it deletes | Configuration key |
   | ------- | --------------- | ----------------- |
   | **Messages** | Text and data messages, with their raw bytes | `dmr.retention.messages` |
   | **Transmissions** | Last Heard entries for voice, data and control traffic | `dmr.retention.transmissions` |
   | **Raw bursts** | The raw DMR bursts of each transmission | `dmr.retention.bursts` |

3. Click **Save**.

The Setup page offers **Keep forever**, **30 days**, **90 days**, **1 year**, **2 years** and **5 years**. In the configuration, each value is a number of days from 0 to 36500, and 0 keeps the records forever. A value from the configuration file that is not in the list shows as a number of days.

DMRHub deletes old records once each hour. With several replicas, only one replica deletes in each hour. Retention also deletes records from the admin search. Set a period that your local rules allow.
