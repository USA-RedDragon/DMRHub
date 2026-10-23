# Messages and SMS Storage

DMRHub routes text messages (SMS) and other data messages between radios, like it routes voice. This page tells how DMRHub stores these messages, what users can see, and what admins can see.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

## What the hub stores

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

## For users

### Turn on SMS Storage

SMS Storage is off by default. When SMS Storage is on, the text messages sent to and from the radio IDs that you own appear on your Messages page, up to the 250 newest in each box.

To turn it on:

1. Log in.
2. Go to `<siteurl>/settings` (**Settings** in the navigation bar).
3. Turn on the **SMS Storage** switch.

To turn it off, turn off the switch, then click **Turn off**. Your inbox and outbox are then empty.

### Radio IDs

Your account ID is always on your list of radio IDs. You cannot remove it.

You can add more 7-digit DMR IDs that you own. On the Settings page, type the ID in **Add a radio ID** and click **Add**. RadioID.net must list the ID under your callsign. Hotspot and repeater IDs go on the Repeaters page, not here.

When you remove an ID, the messages to and from that ID go away from your Messages page.

### The Messages page

Go to `<siteurl>/messages` (**Messages** in the navigation bar). The page has two tabs:

- **Inbox**: private messages sent to your radio IDs. Group messages to talkgroups are not in the inbox.
- **Outbox**: messages sent from your radio IDs, including group messages to talkgroups.

Each box shows the 250 most recent messages. New messages appear while the page is open.

The **State** column shows what happened to each message:

| State                | Meaning                                                      |
| -------------------- | ------------------------------------------------------------ |
| Unconfirmed          | The sender did not ask for a reply                           |
| Delivered (ACK)      | The receiving radio accepted the message                     |
| Rejected (NACK)      | The receiving radio refused the message                      |
| Partial (SACK)       | The receiving radio asked for some blocks again              |
| Confirmed, no reply  | The sender asked for a reply, but no reply came              |

**Checksum bad** shows on a message whose IP or UDP checksum failed. The text can have errors.

The **Message Stats** card counts only the messages in your inbox and outbox, so at most 250 each.

## For admins

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
