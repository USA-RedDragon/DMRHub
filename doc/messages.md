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

## Store and forward

| Fact | Value |
| --- | --- |
| Default | Off |
| Where to turn it on | **Settings**, **Store and forward** card |
| What it keeps | Private text messages to the radio IDs that you own, which DMRHub cannot deliver |
| When DMRHub keeps a message | It does not know where the radio is, or the sender asked for a reply and none came in 10 seconds |
| What it does not keep | Group messages, data that is not text, and messages with a bad CRC |
| When DMRHub sends it | When it hears the radio again on a hotspot or repeater that private calls to it can go to, after the radio is quiet for 2 seconds |
| Format | Only the text format that the radio itself sends, learned from the last private text that the radio sent through a hotspot or repeater of its owner, or one that the admin trusts |
| How long | 7 days by default; the admin sets it |
| How many | 50 waiting messages per user by default; the admin sets it |
| When you turn it off | DMRHub keeps no more messages, and the waiting ones are not sent |

DMRHub sends a waiting message from the ID that sent it. When your radio asks for a reply in its own messages, DMRHub sends the waiting message the same way and waits for the reply. A message with no reply stays waiting, up to 5 tries.

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
| Held for the radio  | Store and forward keeps the message until DMRHub hears the radio |
| Held: radio format not known yet | Store and forward keeps the message, and the radio has not yet sent a text from which DMRHub can learn its format |
| Delivered later     | DMRHub sent the kept message when it heard the radio |
| Delivered later (ACK) | DMRHub sent the kept message and the radio accepted it |
| Not delivered       | The time limit ended, the radio did not answer 5 tries, or the text does not fit the format of the radio |
| Not delivered (NACK) | The radio refused the kept message |

**Checksum bad** shows on a message whose IP or UDP checksum failed. The text can have errors.

## Message Stats

The **Message Stats** card counts only the messages in your inbox and outbox, so at most 250 each. It shows the totals, the count of each format, and the count of each day of the last 7 days, in the time zone of your browser.

## For apps

An app that you allow with the `messages:read` scope can read your messages, and gets new ones on the websocket `/ws/messages` as they arrive.
