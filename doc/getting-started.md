# Get started

This page is for a ham who wants to use a DMRHub network. It tells you what DMRHub is, how to get an account, and how to connect your first hotspot.

In these docs, `<siteurl>` is the web address of the DMRHub network that you use.

## What DMRHub is

DMRHub is a DMR network server. It does the same job as the master servers of networks such as BrandMeister and TGIF, but it is one program that a club or a group can run for itself.

A DMRHub network has:

- **Talkgroups.** Your hotspot or repeater carries a talkgroup on a timeslot, and everyone on that talkgroup hears your call.
- **Private calls and text messages.** DMRHub sends them to the hotspot or repeater where the other radio was last heard.
- **A web site** at `<siteurl>`. On it you register, enroll your hotspots, pick your talkgroups, see who is on the air, read your text messages and see the positions of your radios.
- **dmr-web**, a DMR radio that runs in a web browser, if your network offers it.

Each DMRHub network is separate. An account on one network does not work on another.

## What you need

- An amateur radio license.
- A DMR ID from [RadioID.net](https://radioid.net/), registered to your callsign.
- A DMR radio and a hotspot or repeater that uses the MMDVM network protocol (for example a Pi-Star hotspot), or a web browser for dmr-web.

## Step 1: Get an account

1. Go to `<siteurl>/register` (**Register** in the navigation bar).
2. Enter your DMR ID, a username, your callsign and a password, then click **Create User**.
3. Wait for an admin to approve your account, unless the network approves new accounts at once. The message after you register tells you which.
4. Log in at `<siteurl>/login`.

See [Create an account](guides/create-account.md) for the details and the error messages.

## Step 2: Enroll your hotspot

1. Log in, then go to **Repeaters** and click **Enroll New Repeater**.
2. Enter your DMR ID, or your DMR ID with two more digits (for example 300000101 for a second hotspot), and click **Save**.
3. Copy the configuration that DMRHub shows. It has the password of the hotspot, and DMRHub shows it only once.

## Step 3: Point the hotspot at DMRHub

Put the address, port, ID and password from step 2 in your hotspot. Then make a test call to the parrot on talkgroup 9990, if your network has one, to check that it works.

See [Connect a hotspot](guides/connect-hotspot.md) for Pi-Star and DMRGateway steps, and [Test with the parrot](guides/parrot.md).

## Next steps

- [Pick your talkgroups](guides/talkgroups.md)
- [Make a private call](guides/private-calls.md)
- [Send and read text messages](guides/text-messages.md)
- [Share the position of your radio](guides/positions.md)
- [Use dmr-web, the DMR radio in your browser](guides/dmr-web.md)
- [All user guides](User-Guide.md)
