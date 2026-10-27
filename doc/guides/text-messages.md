# Send and read text messages

DMRHub routes text messages (SMS) between radios like it routes voice. You send and receive them on your radio. You can also read the messages of your radio IDs on the DMRHub web site, if you turn on SMS Storage.

## Send a text message

1. On your radio, write a message.
2. Send it to the DMR ID of the other radio, as a private message. To send to everyone on a talkgroup, send it to the talkgroup as a group message.

A private message reaches the other radio when it has an account on the network and DMRHub heard it in the last 15 minutes. See [How DMRHub finds the other radio](private-calls.md#how-dmrhub-finds-the-other-radio).

Send the message in a text format that the other radio reads. Radios of different makes often use different formats. The [Messages reference](../messages.md#formats) lists the formats that DMRHub can show.

## Turn on SMS Storage

SMS Storage is off by default. While it is off, DMRHub keeps no copy of your messages for the web site.

1. Log in.
2. Go to **Settings** (`<siteurl>/settings`).
3. In the **SMS Storage** card, turn on the switch.

<!-- screenshot: settings-sms-storage.png | the SMS Storage card on /settings with the switch on -->

To turn it off, turn off the switch, then click **Turn off**. Your inbox and outbox are then empty.

## Add your other radio IDs

The Messages page shows the messages of your account ID and of the other radio IDs that you add. To add one:

1. Go to **Settings**.
2. In the **Radio IDs** card, type a 7-digit DMR ID in **Add a radio ID** and click **Add**.

RadioID.net must list the ID under your callsign. Hotspot and repeater IDs go on the Repeaters page, not here. When you remove an ID, its messages go away from your Messages page.

## Read your messages

1. Go to **Messages** (`<siteurl>/messages`).
2. Click **Inbox** for private messages to your radio IDs, or **Outbox** for messages from your radio IDs, group messages too.

<!-- screenshot: messages-inbox.png | the Messages page at /messages with the Inbox tab open and two messages -->

New messages appear while the page is open. Each box shows the 250 newest messages. The **State** column shows whether the other radio accepted the message. See the [Messages reference](../messages.md) for what each column means.

## Read messages in an app

An app that you allow with the `messages:read` scope can read your messages and get new ones as they arrive. dmr-web is such an app. See [Use dmr-web](dmr-web.md) and [Manage your account](account.md#apps).
