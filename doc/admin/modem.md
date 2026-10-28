# MMDVM modem on a serial port

DMRHub can drive an MMDVM modem board on a serial port, such as an MMDVM_HS hat, an MMDVM repeater board or a DVMega. Then DMRHub is a hotspot or a repeater by itself, with no MMDVMHost. To the network, the modem is one more repeater: its calls go to the hub, and the hub routes calls to it like to any other repeater.

Set the modem on the **Modem** tab of the Setup page (`<siteurl>/admin/setup#modem`), or in the configuration file, with environment variables or with flags. See the `dmr.modem.*` keys in [Configuration](configuration.md#configuration-reference). A save on the Setup page restarts the modem, and calls on it stop for a few seconds.

## Set up the modem

1. Pick the ID that the modem will use. You can enroll the repeater first: go to **Repeaters**, click **Enroll New Repeater**, and enter the ID. When no repeater has the ID, DMRHub makes one with no owner when the modem connects, and an admin assigns its owner on the Unassigned Repeaters page (see [Assign an owner](repeaters.md#assign-an-owner)). When the modem connects, DMRHub makes this repeater a modem repeater. After that, DMRHub refuses network logins with this ID.
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

   A simplex hotspot sends and receives on the same frequency, so `rx-frequency` and `tx-frequency` are the same. It has one channel, which is timeslot 2 on the network. Calls on timeslot 1 do not go to it.

   Set `duplex: true` for a duplex repeater. It receives on `rx-frequency` and transmits on `tx-frequency`, uses both timeslots, and repeats what it hears.

4. Restart DMRHub. The log shows `Modem server started` with the port and the repeater ID.

The other keys set the levels, the offsets, the inversions and the delays of the board, as in MMDVMHost. The defaults suit most MMDVM_HS hats. If the board has a frequency error, set `rx-offset` and `tx-offset` in Hz.

## Test the modem

On the **Modem** tab:

- **Serial port** lists the serial ports that the server finds: the names in `/dev/serial/by-id` first, which stay the same after a reboot, then the `ttyACM`, `ttyUSB`, `ttyAMA` and `ttyS` devices. Choose **Another path** to type a path that is not listed.
- The pill in the header of the **Modem** card shows the live state of the modem, from the replica that drives it.
- **Test the modem** opens the serial port with the settings on the form, asks the board for its firmware, and sends it the frequencies and the settings. It saves nothing. It shows the firmware, the protocol, the modes of the board, and whether the board took the frequencies and the settings. While the modem runs on that port, the test does not open the port and shows the live state instead.

If the board does not answer, check the port and its speed, and that no other program, such as MMDVMHost, uses the port.

With more than one replica, the list shows the ports of each replica, under the name of the replica. The test runs on the replica of the port that you choose. For a path that you type, choose its replica next to the path. If that replica does not answer in time, the test says so.

## Signal strength (RSSI)

The board reports a raw signal reading for each call. To show the signal strength of calls in dBm, DMRHub needs an RSSI.dat file in the MMDVMHost format. Without one, the signal strength of modem calls is not shown.

1. Start from the [RSSI.dat](https://github.com/g4klx/MMDVM-Host/blob/master/RSSI.dat) of MMDVMHost. It holds only comments.
2. Add one line for each point: the raw value, then its dBm, apart by spaces or tabs, such as `1134 -90`. DMRHub works out the values between the points. Lines that start with `#` are comments. The points depend on the board. If you used MMDVMHost with the same board, use its RSSI.dat as it is.
3. Put the path of the file in **RSSI mapping file** on the **Modem** tab, or set `dmr.modem.rssi-mapping-file`. DMRHub reads the file each time the modem starts.

## Limits

- Turn the modem on in one replica only. If two replicas try to drive the same repeater ID, the second one logs `Another replica claimed the repeater of the modem; turn the modem on in one replica only`.
- A modem stays on its host. When DMRHub stops, even in a rolling restart, the modem stops too.
- After DMRHub opens the port, it waits 2 seconds for boards that reset. When the link goes down, DMRHub opens the port again after 2 seconds. When an open fails, it tries again every 5 seconds.
