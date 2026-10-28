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

## Slot locks

The owner of a repeater, and an admin, can lock a timeslot to a list of users, for example to keep it open for emergency traffic. A locked timeslot carries only the traffic from the radios of those users, and private traffic to them. See [Set the rules of your repeater](../guides/repeater-rules.md).

## Talkgroup lists

The owner of a repeater, and an admin, can block talkgroups on the repeater, or allow only some talkgroups. See [Set the rules of your repeater](../guides/repeater-rules.md). An owner can misuse the lists to shut people out of a repeater, for example to keep a community off a shared repeater. Admins can see and change the lists of every repeater.

## Repeaters for each user

A user can add at most 100 repeaters and hotspots. When a user has that many, DMRHub refuses another one with the message "You have 100 repeaters and hotspots, the most that a user can add". Admins have no limit, and an admin can still assign a repeater to a user who has that many. To change the limit, set `dmr.repeater-quota`; `0` is no limit. A change applies at once on every replica, and a user who has more than a new limit keeps the repeaters and hotspots they have. See [Configuration](configuration.md).
