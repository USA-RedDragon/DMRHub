# Repeaters

The **Repeaters** page in the **Admin** menu (`<siteurl>/admin/repeaters`) lists every repeater and hotspot on the network, not only your own. For how a user enrolls a hotspot or a repeater, see [Connect a hotspot](../guides/connect-hotspot.md) and [Link a repeater](../guides/link-repeater.md).

For each repeater the table shows the **DMR Radio ID**, the **Type** (MMDVM or IPSC), **Last Connected**, **Last Ping**, the static and dynamic talkgroups of each timeslot, **Simplex**, **Trusted** and **Created**.

<!-- screenshot: admin-repeaters.png | /admin/repeaters with one MMDVM repeater 310001, showing the Edit and Delete buttons and the Trusted column -->

## Edit or delete a repeater

1. Click **Edit** in the row of the repeater.
2. Change the static talkgroups and the dynamic talkgroup of each timeslot, the **Simplex** box, or the **Trusted** box.
3. Click **Save**, or **Cancel** to keep the old values.

To delete a repeater, click **Edit**, then **Delete**, then click **OK** in the browser message.

## Trusted repeaters

DMRHub sends a private call or a text message to the repeater where the radio was last heard. For a radio ID that a user owns, it does this only when the user owns that repeater, or when an admin marked the repeater as trusted. Otherwise the private traffic does not follow the radio to that repeater. The `anyone` reach turns this check off. See [Private call routing](private-calls.md).

Mark a repeater as trusted when it is a shared repeater of the network that users who do not own it use, for example a club repeater. To do this, click **Edit**, turn on the **Trusted** box, and click **Save**. Only an admin can change this box.
