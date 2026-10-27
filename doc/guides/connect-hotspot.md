# Connect a hotspot

This guide tells you how to enroll a hotspot on DMRHub and connect it. DMRHub treats a hotspot and a repeater the same way. For a repeater with its own 6-digit ID, or a Motorola repeater, see [Link a repeater](link-repeater.md).

Your hotspot connects with the MMDVM network protocol, the same protocol that MMDVMHost and DMRGateway use for BrandMeister and TGIF.

## Pick the hotspot ID

A hotspot ID is one of these:

- Your 7-digit DMR ID, for example 3100001.
- Your 7-digit DMR ID with two more digits, from 00 to 99, for example 310000101. Use this form when you have more than one hotspot.

The ID must start with the DMR ID of your account, unless the admin turned off the RadioID.net checks.

## Enroll the hotspot

1. Log in.
2. Go to **Repeaters** (`<siteurl>/repeaters`) and click **Enroll New Repeater**.
3. If the network accepts Motorola repeaters, set **Repeater Type** to **MMDVM**. If it does not, the page does not show this field.
4. Enter the hotspot ID in **DMR Radio ID**.
5. Leave **Simplex Repeater** off for a normal hotspot. When it is on, DMRHub sends what it hears on one timeslot back on the other timeslot.
6. Click **Save**.

<!-- screenshot: new-repeater.png | the New Repeater form at /repeaters/new with DMR Radio ID 310000101 -->

DMRHub then shows a DMRGateway configuration, like this one:

```ini
[DMR Network 2]
Name=AREDN
Enabled=1
Address=dmrhub.example.org
Port=62031
Password="kG7tZ2pQ9wX4mN8v"
Id=310000101
Location=1
Debug=0
```

> **Important:** Copy the password now. DMRHub shows it only once. If you lose it, delete the hotspot on the Repeaters page and enroll it again.

The address is the host name of the DMRHub web site, and the port is the MMDVM port of the network (62031 by default). If your admin runs the MMDVM server on another address, use the address your admin gives you.

## Connect a Pi-Star hotspot

Pi-Star lists the DMR masters that it can connect to in a host file. To add DMRHub, you add your own line to that file. These steps come from the Pi-Star dashboard source; screens can differ between Pi-Star versions.

1. In the Pi-Star dashboard, open **Expert**, then **SSH Access**, and log in.
2. Make the file system writable:

   ```sh
   rpi-rw
   ```

3. Add a line for DMRHub to `/root/DMR_Hosts.txt`. Pi-Star adds this file to its host list each time it updates the host files. The columns are a name with no spaces, `0000`, the address, the password and the port:

   ```text
   DMRHub_Local	0000	dmrhub.example.org	kG7tZ2pQ9wX4mN8v	62031
   ```

4. Update the host files, so that the new line is in the list:

   ```sh
   sudo HostFilesUpdate.sh
   ```

5. Open **Configuration**. In **General Configuration**, set **CCS7/DMR ID** to your DMR ID. In **DMR Configuration**, set **DMR Master** to `DMRHub_Local`, then click **Apply Changes**. The hotspot logs in with its ID, so the ID that you enrolled on DMRHub must match it: your 7-digit DMR ID, or your DMR ID with the two ESSID digits if you set an ESSID.
6. Pi-Star keeps a password that MMDVMHost already has. Open **Expert**, then **MMDVMHost** under **Quick Edit**, and check that **Password** in the **DMR Network** section is the DMRHub password. Change it and apply if it is not.

<!-- screenshot: pistar-dmr-master.png | the Pi-Star Configuration page with DMR Master set to DMRHub_Local -->

When the hotspot connects, the **Last Connected** and **Last Ping** columns on the Repeaters page show a time.

## Connect with DMRGateway

If you run DMRGateway yourself, add the section that DMRHub showed to `DMRGateway.ini`. Use a network number that is free in your file.

DMRGateway sends traffic to a network only when a rewrite or pass rule of that network matches. When DMRHub is the only network, add these lines to its section, so that all group and private calls on both timeslots go to DMRHub:

```ini
PassAllTG=1
PassAllTG=2
PassAllPC=1
PassAllPC=2
```

When DMRHub shares DMRGateway with other networks, write rewrite rules (`TGRewrite`, `PCRewrite`) that send only the talkgroups you want to DMRHub. See the DMRGateway documentation for the rule format.

## Other hotspot software

Any software that speaks the MMDVM network protocol works. Give it these values:

| Setting | Value |
| --- | --- |
| Address | The host name of `<siteurl>`, or the address your admin gives you |
| Port | The port that DMRHub showed (62031 by default) |
| ID | The hotspot ID that you enrolled |
| Password | The password that DMRHub showed once |

## Check the connection

1. Go to **Repeaters** and find your hotspot.
2. **Last Connected** shows when the hotspot logged in. **Last Ping** shows the last keepalive.
3. Click the hotspot ID to open its page. It shows **Online** or **Offline**, the frequencies, color code and software that the hotspot sent, and the last calls through it.
4. Call the parrot to test your audio. See [Test with the parrot](parrot.md).

<!-- screenshot: repeater-details.png | the repeater page at /repeaters/310000101 of a connected hotspot -->

## Delete a hotspot

1. Go to **Repeaters**.
2. Click **Edit** on the row of the hotspot.
3. Click **Delete**, then confirm.

## Next

- [Pick your talkgroups](talkgroups.md)
- [Use Codec2 radios](voice-codec.md)
