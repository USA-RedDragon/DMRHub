# Use Codec2 radios

Most DMR radios use the AMBE voice codec. Some radios and apps, such as dmr-web, can use Codec2 instead. DMRHub can convert calls between the two when the network has AMBE servers for transcoding. Ask your admin whether it does. Without transcoding, each call passes through in its own codec, and a radio hears only calls in the codec that it uses.

## Set the codec of a timeslot

Each timeslot of an MMDVM hotspot or repeater has a codec setting. The owner of the hotspot and admins can change it.

1. Go to **Repeaters** and click the ID of your hotspot.
2. In the **Voice codec** card, pick the **Codec** of **Timeslot 1** and **Timeslot 2**:
   - **Automatic**: follows the last call heard on the timeslot. A timeslot counts as Codec2 only when a call starts with a Codec2 voice header. Until then it is AMBE. The card shows the codec of the last call, for example "Heard: Codec2 3200, on the last call at 14:02 CDT."
   - **Codec2**: the radios on this timeslot use Codec2.
   - **AMBE**: the radios on this timeslot use AMBE.
3. Click **Save**.

<!-- screenshot: voice-codec.png | the Voice codec card on /repeaters/300000101 with Timeslot 2 set to Codec2 -->

DMRHub then sends AMBE calls to a Codec2 timeslot as Codec2, and Codec2 calls to AMBE timeslots as AMBE. IPSC repeaters do not have this card.

## Read the Codec column

The **Codec** column on the Last Heard page shows the codec of each voice call, and what DMRHub did with it:

| Text | Meaning |
| --- | --- |
| Sent as AMBE to N targets | DMRHub sent the call as AMBE to that many hotspots or repeaters. |
| Sent as Codec2 to N targets | DMRHub sent the call as Codec2 to that many hotspots or repeaters. |
| Not converted: every AMBE server was busy | No AMBE server was free, so the call did not reach the timeslots that use the other codec. |
| N s not converted: AMBE server did not answer | Part of the call did not reach the timeslots that use the other codec. |
