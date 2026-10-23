# Messages and SMS Storage

DMRHub routes text messages (SMS) and other data messages between radios, like it routes voice. This page tells how to see the text messages of your radio IDs on DMRHub.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

## Turn on SMS Storage

SMS Storage is off by default. When SMS Storage is on, the text messages sent to and from the radio IDs that you own appear on your Messages page, up to the 250 newest in each box.

To turn it on:

1. Log in.
2. Go to `<siteurl>/settings` (**Settings** in the navigation bar).
3. Turn on the **SMS Storage** switch.

To turn it off, turn off the switch, then click **Turn off**. Your inbox and outbox are then empty.

## Radio IDs

Your account ID is always on your list of radio IDs. You cannot remove it.

You can add more 7-digit DMR IDs that you own. On the Settings page, type the ID in **Add a radio ID** and click **Add**. RadioID.net must list the ID under your callsign. Hotspot and repeater IDs go on the Repeaters page, not here.

When you remove an ID, the messages to and from that ID go away from your Messages page.

## The Messages page

Go to `<siteurl>/messages` (**Messages** in the navigation bar). The page has two tabs:

- **Inbox**: private messages sent to your radio IDs. Group messages to talkgroups are not in the inbox.
- **Outbox**: messages sent from your radio IDs, including group messages to talkgroups.

Each box shows the 250 most recent messages. New messages appear while the page is open.

The **Format** column shows one of these formats:

| Format          | Description                                                  |
| --------------- | ------------------------------------------------------------ |
| ETSI IP         | Text in an IP and UDP packet                                 |
| Motorola TMS    | Motorola Text Messaging Service                              |
| AnyTone         | AnyTone text messages                                        |
| Hytera          | Hytera text messages                                         |
| Defined Data    | Text sent as ETSI defined short data                         |
| Data (not text) | A data message that is not text, for example location data   |

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
