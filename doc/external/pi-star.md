# Pi-Star

Checked 2026-10-28 on a real Pi-Star hotspot that is connected to a DMRHub server: Pi-Star 4.1.6, dashboard 20220717 (git 04be295), MMDVMHost 20210921 (git b1d5973), DMRGateway 20220106_PS4 (git 5aa6e54). We read its configuration, its logs and its dashboard source. We did not change its configuration.

| What | How we checked it | Result |
| --- | --- | --- |
| [Through DMRGateway](#through-dmrgateway): the values in the files | The running `/etc/mmdvmhost` and `/etc/dmrgateway`, and the DMRGateway and MMDVMHost logs | Worked. DMRGateway logged in to DMRHub and answered pings, and a private call to the parrot through the rewrite rules came back. |
| [Through DMRGateway](#through-dmrgateway): the dashboard steps | The dashboard source on the hotspot | Not run. The steps come from the source. |
| [Direct to DMRHub](#direct-to-dmrhub) | The dashboard source on the hotspot | Not run. The hotspot does not use this mode. |

For the steps with your values filled in, open the connect page of the hotspot on DMRHub. See [Connect a hotspot](../guides/connect-hotspot.md#set-up-your-software).

## What it is

Pi-Star runs MMDVMHost, DMRGateway and the other MMDVM gateways on a Raspberry Pi with a modem hat, and gives them a web dashboard. See [MMDVMHost](mmdvmhost.md) and [DMRGateway](dmrgateway.md).

Pi-Star connects to DMRHub in one of two modes:

- **Through DMRGateway.** The **DMR Master** is `DMRGateway`. MMDVMHost sends to DMRGateway, and DMRGateway logs in to DMRHub and to other networks at the same time.
- **Direct.** The **DMR Master** is DMRHub. MMDVMHost logs in to DMRHub itself, and DMRHub is the only DMR network of the hotspot.

## What to set in DMRHub

Enroll the ID that the hotspot logs in with at `<siteurl>/repeaters/new`, as for any hotspot, and copy the password. The ID is your 7-digit DMR ID, or your DMR ID and the two digits of the ESSID, such as `300000101`. See [Connect a hotspot](../guides/connect-hotspot.md).

## Add DMRHub to the host list

The dashboard makes its master lists from `/usr/local/etc/DMR_Hosts.txt` (admin/configure.php:4011). Pi-Star downloads that file again when it updates its host files, so add your line to `/root/DMR_Hosts.txt`. `HostFilesUpdate.sh` adds that file to the end of the list each time it runs (HostFilesUpdate.sh:129-131).

Each line has five fields, with white space between them: name, ID, address, password and port (admin/configure.php:4031-4035). The dashboard does not use the ID field.

1. In the dashboard, open **Configuration**, then **Expert**, then **SSH Access**, and log in as `pi-star`. You can also use `ssh pi-star@<hotspot address>`.
2. Make the root file system writable with `rpi-rw`. It is read-only by default. `rpi-rw` is a shell alias of Pi-Star (`/etc/bash.bashrc`).
3. Add the line and update the host files:

   ```bash
   rpi-rw
   echo -e "DMR+_DMRHub\t0000\tdmrhub.example.org\tkG7tZ2pQ9wX4\t62031" | sudo tee -a /root/DMR_Hosts.txt
   sudo HostFilesUpdate.sh
   ```

The name decides where the line shows:

- The **DMR Master** list shows every line that does not start with `XLX` (admin/configure.php:4033).
- The **DMR+ Master** list, for DMRGateway, shows only the lines that start with `DMR+` (admin/configure.php:4127).
- A name that starts with `DMR+`, `HB_` or `FD_` adds a **DMR Options** row in direct mode (admin/configure.php:4310).

So a name such as `DMR+_DMRHub` works in both modes.

The dashboard passes the line through `escapeshellcmd` when you apply it (admin/configure.php:1182, 1292). That puts a `\` in front of characters such as `#`, `&`, `;` and `$`, so a password with one of them does not work. A password with white space breaks the line. Passwords that DMRHub makes now have only letters and digits. If your password is older and has other characters, rotate it. See [Rotate the password](../guides/connect-hotspot.md#rotate-the-password).

## Through DMRGateway

### The values that worked

The hotspot that we checked is a duplex hotspot with DMRHub as network 2 of DMRGateway, next to BrandMeister (network 1) and TGIF (network 3). These are the DMR parts of its running files. The IDs, the address and the password are examples.

`/etc/mmdvmhost`:

```ini
[General]
Id=3000001

[DMR]
Enable=1
Id=3000001

[DMR Network]
Enable=1
Type=Gateway
Address=127.0.0.1
Port=62031
RemoteAddress=127.0.0.1
RemotePort=62031
Local=62032
LocalPort=62032
Slot1=1
Slot2=1
```

`/etc/dmrgateway`:

```ini
[General]
RptAddress=127.0.0.1
RptPort=62032
LocalAddress=127.0.0.1
LocalPort=62031

[DMR Network 2]
Enabled=1
Name=DMRHub
Address=dmrhub.example.org
Port=62031
Password=kG7tZ2pQ9wX4
Id=300000101
Location=1
PCRewrite1=1,8009990,1,9990,1
PCRewrite2=2,8009990,2,9990,1
TypeRewrite1=1,8009990,1,9990
TypeRewrite2=2,8009990,2,9990
TGRewrite1=1,8000001,1,1,999999
TGRewrite2=2,8000001,2,1,999999
SrcRewrite1=1,9990,1,8009990,1
SrcRewrite2=2,9990,2,8009990,1
SrcRewrite3=1,1,1,8000001,999999
SrcRewrite4=2,1,2,8000001,999999
```

These are the network 2 rewrite rules of Pi-Star. With them, your radio calls DMRHub with an `8` in front of the ID, and calls without the `8` go to the other networks. By the rule format:

| Your radio calls | DMRHub gets |
| --- | --- |
| Private call 8009990 | Private call to the parrot, 9990 |
| Talkgroup 8000091 | Talkgroup 91 |
| Talkgroup 8003100 | Talkgroup 3100 |

We saw only the parrot call in the logs. When DMRHub is the only network, use the `PassAllTG` and `PassAllPC` lines of the [DMRGateway](dmrgateway.md) page in place of the rewrite rules.

What the logs showed with these values:

- DMRGateway logged `DMRHub, Logged into the master successfully`, and DMRHub answered each `RPTPING` with `MSTPONG`.
- On 2026-10-26, a private call from the radio to 8009990 went to DMRHub, and the parrot played it back on TG 8009990 with 0% packet loss.
- When DMRHub restarted, DMRGateway logged `DMRHub, Login to the master has failed, retrying login ...` and logged in again 10 seconds later.

### Set it in the dashboard

We did not run these steps. They come from the dashboard source on the hotspot.

1. Add a `DMR+_` line for DMRHub to the host list. See [Add DMRHub to the host list](#add-dmrhub-to-the-host-list).
2. Open **Configuration**. In **DMR Configuration**, set **DMR Master** to `DMRGateway` and click **Apply Changes**. The page then shows the DMRGateway rows.
3. Set these fields and click **Apply Changes**:

   | Field | Value |
   | --- | --- |
   | DMR+ Master | `DMR+_DMRHub` |
   | DMR+ Network ESSID | The two extra digits of the hotspot ID, such as `01`, or `None` for a 7-digit ID |
   | DMR+ Network Enable | On |

The dashboard writes the address, the password, the port and the name of the line to `[DMR Network 2]` (admin/configure.php:1291-1296). It writes your DMR ID and the ESSID to `Id` (admin/configure.php:1162-1164).

> **Important:** In this mode, each **Apply Changes** on the **Configuration** page writes the selected **DMR+ Master** to `[DMR Network 2]`. When the address and the port of network 2 are not a line of the **DMR+ Master** list, no line is selected, the browser sends the first line of the list, and the dashboard replaces DMRHub with that master. So when you set network 2 by hand in **Expert**, **DMR GW**, also add a `DMR+_` line with the same address and port.

## Direct to DMRHub

We did not run this mode. What follows comes from the dashboard source on the hotspot.

1. Add DMRHub to the host list. See [Add DMRHub to the host list](#add-dmrhub-to-the-host-list).
2. Open **Configuration** and set these fields:

   | Section | Field | Value |
   | --- | --- | --- |
   | General Configuration | Node Callsign | Your callsign |
   | General Configuration | CCS7/DMR ID | Your 7-digit DMR ID |
   | DMR Configuration | DMR Master | The DMRHub line, such as `DMR+_DMRHub` |
   | DMR Configuration | DMR ESSID | The two extra digits of the hotspot ID, such as `01`, or `None` for a 7-digit ID |
   | DMR Configuration | DMR Color Code | The color code of your radio |

3. Click **Apply Changes**.

The dashboard writes the address and the port of the line to `Address`, `RemoteAddress`, `Port` and `RemotePort` in the `[DMR Network]` section of MMDVMHost, and sets `Type=Direct` (admin/configure.php:1181-1194). Only the master `127.0.0.1` port 62031 sets `Type=Gateway`. The DMR ESSID sets `Id` of the `[DMR]` section to your DMR ID and the two digits (admin/configure.php:1150-1153). MMDVMHost logs in with that ID.

The password depends on the dashboard version:

- Dashboard 20220717 writes the password of the line each time you apply a master (admin/configure.php:1185).
- The current dashboard (github.com/AndyTaylorTweet/Pi-Star_DV_Dash, commit ca50804) writes it only when `[DMR Network]` has no password yet (admin/configure.php:1517-1518).

So after you change the master, open **Configuration**, **Expert**, **MMDVMHost**, and check that **Password** in **DMR Network** is the DMRHub password.

The **DMR Color Code** label is **DMR Colour Code** when the dashboard language is UK English.

## Check that it works

- On DMRHub, open `<siteurl>/repeaters/<hotspot ID>`. **Last Connected** and **Last Ping** must show the current time.
- On the hotspot, the DMRGateway log, `/var/log/pi-star/DMRGateway-<date>.log`, shows `Logged into the master successfully` after the name of the network. The log times are in UTC.
- Call the parrot: 9990 in direct mode, or 8009990 through DMRGateway with the rewrite rules above.
