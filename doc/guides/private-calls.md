# Make a private call

A private call goes to one radio, not to a talkgroup. On DMRHub you can make a private call to another ham on the same network, or to a hotspot or repeater.

## How DMRHub finds the other radio

DMRHub sends a private call to a radio ID that has an account on the network. It sends the call to the hotspot or repeater where that radio was last heard in the last 15 minutes. DMRHub drops the call when:

- the ID has no account on this network, or
- DMRHub did not hear that radio in the last 15 minutes.

So the other ham must have transmitted recently, for example with a key-up, a call or an ARS registration from the radio. A radio that registers with ARS when it turns on is easier to reach.

A private call to the 6-digit ID of a repeater on the network goes to that repeater. DMRHub treats a 7-digit ID as a radio, even when a hotspot uses it as its ID.

## Make the call

1. Add the DMR ID of the other ham as a private call contact in your radio, for example 3100002.
2. Pick that contact and make the call on the timeslot that your hotspot uses.
3. Wait for the other ham to answer. The answer comes to you as a private call.

A private call does not link a dynamic talkgroup.

## See your private calls

When you are logged in, the **Last Heard** page shows new calls from you and to you as they happen. The page of your hotspot (click its ID on the Repeaters page) lists the calls through it.

## Next

[Send and read text messages](text-messages.md). Text messages find the other radio in the same way.
