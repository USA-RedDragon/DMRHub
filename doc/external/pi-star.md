# Pi-Star

**Untested.** We could not run Pi-Star on this test host:

- Pi-Star is a disk image for a Raspberry Pi. Its MMDVMHost and DMRGateway are binaries of its own builds.
- The source of the Pi-Star MMDVMHost is not public. `github.com/AndyTaylorTweet/MMDVMHost` returns 404.

What this page says comes from the source of the Pi-Star dashboard (github.com/AndyTaylorTweet/Pi-Star_DV_Dash, commit ca50804), not from a run. For the steps that a ham follows, see [Connect a Pi-Star hotspot](../guides/connect-hotspot.md#set-up-your-software).

## What it is

Pi-Star runs MMDVMHost, DMRGateway and the other MMDVM gateways on a Raspberry Pi with a modem hat, and gives them a web dashboard. See [MMDVMHost](mmdvmhost.md) and [DMRGateway](dmrgateway.md) for the programs that we did test.

## How Pi-Star picks the DMR master

- The **DMR Master** list of the configuration page comes from `/usr/local/etc/DMR_Hosts.txt` (admin/configure.php:4489). Each line has a name, an ID, the address, the password and the port, with white space between them (admin/configure.php:4510-4513).
- When you apply a master, Pi-Star writes its address and port to `Address` and `Port` in the `[DMR Network]` section of the MMDVMHost configuration (admin/configure.php:1513-1521).
- Pi-Star writes the password of the host line only when that section has no password yet (admin/configure.php:1517-1519). So when you change the master, check the password by hand.
- A master other than `127.0.0.1` port 62031 makes `Type=Direct`, and the master `127.0.0.1` port 62031 makes `Type=Gateway` (admin/configure.php:1522-1528). We infer from these names that with `Direct`, MMDVMHost logs in to DMRHub itself, and with `Gateway`, DMRGateway does the login. We could not check this.

## What to set in DMRHub

Enroll the ID that the hotspot logs in with at `<siteurl>/repeaters/new`, as for any hotspot. See [DMRGateway](dmrgateway.md#what-to-set-in-dmrhub).

## Check that it works

Open `<siteurl>/repeaters/<hotspot ID>`. **Last Connected** and **Last Ping** must show the current time.
