# Run or join a net

A net is a session on a talkgroup where hams check in. DMRHub counts a check-in each time a ham with an account on the network makes a voice call on the talkgroup while the net is active.

## Find a net

Go to **Nets** (`<siteurl>/nets`). Anyone can open this page. It shows the featured nets, the upcoming scheduled nets, and the active and past nets with their check-in counts.

<!-- screenshot: nets.png | the Nets page at /nets with one active net and one upcoming scheduled net -->

The page of a talkgroup (**Talkgroups**, then click the talkgroup) also shows its active net and its scheduled nets.

## Check in

1. Link the talkgroup of the net on your hotspot. See [Pick your talkgroups](talkgroups.md).
2. When the net control operator calls for check-ins, make a voice call on the talkgroup and give your callsign.

DMRHub adds your check-in to the net. The check-in counts only when your radio ID is the account ID of a user on the network.

## See the check-ins

1. Click the net on the Nets page, or click **View Check-Ins** on the page of the talkgroup.
2. The net page shows when the net started, who started it, its status, and the users who checked in.
3. When you are logged in, click **Export CSV** or **Export JSON** to save the list.

## Start a net now

1. Log in and open the page of the talkgroup.
2. Click **Start Net**.
3. Type an optional description, and an optional duration in minutes (1 to 1440).
4. Click **Start**.

A talkgroup has at most one active net. To end the net, click **Stop Net** on the page of the talkgroup. Only an admin of the talkgroup, a net control operator (NCO) of the talkgroup, or a network admin can stop a net.

## Schedule a net

1. Log in and open the page of the talkgroup, then click **Schedule Net**. Or go to `<siteurl>/nets/scheduled/new`.
2. Fill in **Name**, **Talkgroup**, **Day of Week**, **Time**, and optionally **Description** and **Duration**. The day and time are in the time zone of your browser.
3. Leave **Enabled** on, and click **Create**.

<!-- screenshot: scheduled-net-form.png | the New Scheduled Net form at /nets/scheduled/new filled in for a weekly net on TG 3100 -->

DMRHub starts the net at that time each week. The page of the talkgroup lists the scheduled nets in UTC. Talkgroup admins, NCOs and network admins can click **Edit** or **Delete** on a scheduled net.

## Become a net control operator

An admin of the talkgroup picks its NCOs on the **Owned** talkgroups page. See [Talkgroups you manage](talkgroups.md#talkgroups-you-manage).
