# Motorola IPSC repeaters

DMRHub can be the IPSC master of Motorola repeaters, such as the XPR 8400 and the SLR 5500. Each repeater has its own auth key. For the steps that the owner of the repeater does, see [Link a repeater](../guides/link-repeater.md#motorola-ipsc-repeaters).

## Turn on IPSC

1. Go to **Admin**, then **Setup**, and find the **IPSC Settings** card in **DMR Settings**.
2. Turn on **Enabled**.
3. Set the **Network ID**. This is the DMR ID that DMRHub uses as the master to the repeaters. It is required.
4. Set the **Port** (default 50000, UDP), and open it in your firewall.
5. Click **Save**.

The configuration keys are `dmr.ipsc.enabled`, `dmr.ipsc.bind`, `dmr.ipsc.port` and `dmr.ipsc.network-id`.

When IPSC is on, **Motorola IPSC** shows in the **Repeater Type** field of the **Enroll New Repeater** page. DMRHub makes a 40-digit hex auth key for each IPSC repeater and shows it once.

<!-- screenshot: admin-setup-ipsc.png | /admin/setup, the IPSC Settings card with Enabled on, port 50000 and network ID 310999 -->

## Hold times for data after a call

An IPSC repeater carries one call on a timeslot at a time. After a call ends on a slot, DMRHub waits before it sends a data call or a CSBK to that slot, so that the repeater is ready for it:

| Key | Default | What it does |
| --- | ------- | ------------ |
| `dmr.ipsc.call-hang-hold` | `2.5s` | The wait after a voice call |
| `dmr.ipsc.data-hang-hold` | `1.2s` | The wait after a data call |

Each value is from 0 to 10 seconds. 0 turns the wait off. These two settings are not on the Setup page. Set them in the configuration file, or with `DMR_IPSC_CALL_HANG_HOLD` and `DMR_IPSC_DATA_HANG_HOLD`.

When a slot of an IPSC repeater is busy, DMRHub drops the bursts for that slot and marks them `slot_busy` on the Last Heard entry. See [Transmission kinds](transmission-kinds.md).
