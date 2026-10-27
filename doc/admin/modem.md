# MMDVM modem on a serial port

DMRHub can drive an MMDVM modem board on a serial port, such as an MMDVM_HS hat, an MMDVM repeater board or a DVMega. Then DMRHub is a hotspot or a repeater by itself, with no MMDVMHost. To the network, the modem is one more repeater: its calls go to the hub, and the hub routes calls to it like to any other repeater.

Set the modem on the **Modem** tab of the Setup page (`<siteurl>/admin/setup#modem`), or in the configuration file, with environment variables or with flags. See the `dmr.modem.*` keys in [Configuration](configuration.md#configuration-reference). A save on the Setup page restarts the modem, and calls on it stop for a few seconds.

## Set up the modem

1. Pick the ID that the modem will use. You can enroll the repeater first: go to **Repeaters**, click **Enroll New Repeater**, and enter the ID. When no repeater has the ID, DMRHub makes one with no owner when the modem connects, and an admin assigns its owner. When the modem connects, DMRHub makes this repeater a modem repeater. After that, DMRHub refuses network logins with this ID.
2. Give DMRHub access to the serial device. On most Linux systems the device belongs to the `dialout` or `uucp` group: add the user that runs DMRHub to that group. In Docker, add `--device /dev/ttyAMA0` (or your device) and `--group-add` with the group ID of the device.
3. Add the modem settings. For example, for a simplex hotspot on 438.800 MHz:

   ```yaml
   dmr:
     modem:
       enabled: true
       port: /dev/ttyAMA0
       repeater-id: 300000101
       rx-frequency: 438800000
       tx-frequency: 438800000
       color-code: 1
   ```

   Set `duplex: true` for a duplex repeater with two timeslots. A simplex hotspot has one channel, which is timeslot 2.

4. Restart DMRHub. The log shows `Modem server started` with the port and the repeater ID.

The other keys set the levels, the offsets, the inversions and the delays of the board, as in MMDVMHost. The defaults suit most MMDVM_HS hats. If the board has a frequency error, set `rx-offset` and `tx-offset` in Hz.

## Limits

- Turn the modem on in one replica only. If two replicas try to drive the same repeater ID, the second one logs `Another replica claimed the repeater of the modem; turn the modem on in one replica only`.
- A modem stays on its host. When DMRHub stops, even in a rolling restart, the modem stops too.
- After DMRHub opens the port, it waits 2 seconds for boards that reset. When the link goes down, DMRHub opens the port again after 2 seconds. When an open fails, it tries again every 5 seconds.
