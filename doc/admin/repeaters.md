# Repeaters

The **Repeaters** page in the **Admin** menu (`<siteurl>/admin/repeaters`) lists every repeater and hotspot on the network, not only your own. For how a user enrolls a hotspot or a repeater, see [Connect a hotspot](../guides/connect-hotspot.md) and [Link a repeater](../guides/link-repeater.md).

For each repeater the table shows the **DMR Radio ID**, the **Type** (MMDVM or IPSC), **Last Connected**, **Last Ping**, the static and dynamic talkgroups of each timeslot, **Simplex**, **Trusted** and **Created**.

<!-- screenshot: admin-repeaters.png | /admin/repeaters with one MMDVM repeater 310001, showing the Edit and Delete buttons and the Trusted column -->

## Edit or delete a repeater

1. Click **Edit** in the row of the repeater.
2. Change the static talkgroups and the dynamic talkgroup of each timeslot, the **Simplex** box, or the **Trusted** box.
3. Click **Save**, or **Cancel** to keep the old values.

To delete a repeater, click **Edit**, then **Delete**, then click **OK** in the browser message.

## Assign an owner

When a modem connects with an ID that no repeater has, DMRHub makes the repeater with no owner (see [MMDVM modem on a serial port](modem.md)). The repeater carries traffic at once. While a repeater has no owner, the **Admin** menu shows **Unassigned** with the number of such repeaters.

1. Go to **Admin**, then **Unassigned** (`<siteurl>/admin/repeaters/unassigned`). The table shows the **ID**, **Kind**, **Callsign**, **Frequencies**, **Color code**, when the repeater last **Connected**, and when DMRHub **Made** it.
2. Click **Assign owner** in the row of the repeater.
3. Find the user by callsign, username or DMR ID, and pick the user.
4. Click **Assign owner**.

The repeater then shows in the repeater list of that user, and the user can set its talkgroups and details. You can assign an owner only to a repeater that has none. When another admin assigned one first, the page shows "Repeater already has an owner".

<!-- screenshot: admin-unassigned-repeaters.png | /admin/repeaters/unassigned with one modem hotspot 311860 and one modem repeater 311861 -->

## Trusted repeaters

DMRHub sends a private call or a text message to the repeater where the radio was last heard. For a radio ID that a user owns, it does this only when the user owns that repeater, or when an admin marked the repeater as trusted. Otherwise the private traffic does not follow the radio to that repeater. The `anyone` reach turns this check off. See [Private call routing](private-calls.md).

The same rule decides which repeaters a radio's positions may come through (see [Positions and APRS-IS](positions.md#what-the-hub-stores)).

Mark a repeater as trusted when it is a shared repeater of the network that users who do not own it use, for example a club repeater. To do this, click **Edit**, turn on the **Trusted** box, and click **Save**. Only an admin can change this box.

## Slot locks

The owner of a repeater, and an admin, can lock a timeslot to a list of users, for example to keep it open for emergency traffic. A locked timeslot carries only the traffic from the radios of those users, and private traffic to them. See [Set the rules of your repeater](../guides/repeater-rules.md).

## Talkgroup lists

The owner of a repeater, and an admin, can block talkgroups on the repeater, or allow only some talkgroups. See [Set the rules of your repeater](../guides/repeater-rules.md). An owner can misuse the lists to shut people out of a repeater, for example to keep a community off a shared repeater. Admins can see and change the lists of every repeater.

## Repeaters for each user

A user can add at most 100 repeaters and hotspots. When a user has that many, DMRHub refuses another one with the message "You have 100 repeaters and hotspots, the most that a user can add". Admins have no limit, and an admin can still assign a repeater to a user who has that many. To change the limit, set **Repeaters for each user** on the **Routing** tab of the Setup page, or `dmr.repeater-quota`; `0` is no limit. A change applies at once on every replica, and a user who has more than a new limit keeps the repeaters and hotspots they have. See [Configuration](configuration.md).
