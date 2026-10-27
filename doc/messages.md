# Messages reference

This page lists the facts about text messages and SMS Storage on DMRHub. For the steps, see [Send and read text messages](guides/text-messages.md).

## SMS Storage

| Fact | Value |
| --- | --- |
| Default | Off |
| Where to turn it on | **Settings**, **SMS Storage** card |
| What it keeps | The text messages sent to and from the radio IDs that you own |
| How many | The 250 newest in each box |
| When you turn it off | Your inbox and outbox are empty |

## Radio IDs

- Your account ID is always on your list of radio IDs. You cannot remove it.
- You can add more 7-digit DMR IDs. RadioID.net must list each one under your callsign, and it must not belong to another user.
- Hotspot and repeater IDs go on the Repeaters page, not on the list of radio IDs.
- When you remove an ID, the messages to and from that ID go away from your Messages page.
- On a network where the admin turned off the RadioID.net checks, RadioID.net does not have to list the ID under your callsign.

| Message | Meaning |
| --- | --- |
| Enter a 7-digit DMR ID | The ID is not a 7-digit DMR ID. |
| N is already on your list | You added this ID before. |
| N belongs to another DMRHub user | Another account has this ID. |
| RadioID.net does not list N under your callsign | RadioID.net lists the ID under another callsign, or not at all. |

## Boxes

| Box | Has |
| --- | --- |
| Inbox | Private messages sent to your radio IDs. Group messages to talkgroups are not in the inbox. |
| Outbox | Messages sent from your radio IDs, group messages to talkgroups too. |

Each box shows the 250 most recent messages. New messages appear while the page is open.

## Columns

| Column | Meaning |
| --- | --- |
| Time | When DMRHub got the message |
| From | The radio that sent it |
| To | The radio or talkgroup that it went to |
| Format | The text format (see below) |
| Text | The text, or "Not text: N octets of data" |
| State | What happened to the message (see below) |

## Formats

| Format          | Description                                                |
| --------------- | ---------------------------------------------------------- |
| ETSI IP         | Text in an IP and UDP packet                               |
| Motorola TMS    | Motorola Text Messaging Service                            |
| AnyTone         | AnyTone text messages                                      |
| Hytera          | Short data in the Hytera text layout, which AnyTone radios send in H-SMS mode |
| Defined Data    | Text sent as ETSI defined short data                       |
| Data (not text) | A data message that is not text, for example location data |
| Unknown         | A format that DMRHub does not know                         |

## States

| State               | Meaning                                         |
| ------------------- | ----------------------------------------------- |
| Unconfirmed         | The sender did not ask for a reply              |
| Delivered (ACK)     | The receiving radio accepted the message        |
| Rejected (NACK)     | The receiving radio refused the message         |
| Partial (SACK)      | The receiving radio asked for some blocks again |
| Confirmed, no reply | The sender asked for a reply, but no reply came |

**Checksum bad** shows on a message whose IP or UDP checksum failed. The text can have errors.

## Message Stats

The **Message Stats** card counts only the messages in your inbox and outbox, so at most 250 each. It shows the totals, the count of each format, and the count of each day of the last 7 days, in the time zone of your browser.

## For apps

An app that you allow with the `messages:read` scope can read your messages, and gets new ones on the websocket `/ws/messages` as they arrive.
