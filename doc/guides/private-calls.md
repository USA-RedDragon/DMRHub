# Make a private call

A private call goes to one radio, not to a talkgroup. On DMRHub you can make a private call to another ham on the same network, or to a hotspot or repeater.

## How DMRHub finds the other radio

DMRHub sends a private call to the hotspot or repeater where it last heard the other radio. The other radio must have transmitted on a hotspot or repeater of this network at least once. With the default settings:

- The other radio needs no account on the network.
- A radio ID that a user owns, the DMR ID of an account or a radio ID that a user added on the Settings page, counts only on a hotspot or repeater of that user, or on one that an admin trusts. A hotspot of someone else that sends your ID cannot pull your calls.
- There is no time limit. DMRHub remembers where it last heard each radio.
- DMRHub drops the call when it never heard the other radio.

Your admin can change these rules: allow only radios of users, follow every radio wherever it was heard, set a time limit, or send calls to a radio that DMRHub does not know to every hotspot and repeater. Text messages can have their own rules. See [Private call routing](../admin/private-calls.md).

A key-up, a call or an ARS registration from the radio tells DMRHub where it is. A radio that registers with ARS when it turns on is easier to reach.

A private call to the 6-digit ID of a repeater on the network goes to that repeater. DMRHub treats a 7-digit ID as a radio, even when a hotspot uses it as its ID.

## Make the call

1. Add the DMR ID of the other ham as a private call contact in your radio, for example 3000002.
2. Pick that contact and make the call on the timeslot that your hotspot uses.
3. Wait for the other ham to answer. The answer comes to you as a private call.

A private call does not link a dynamic talkgroup.

## See your private calls

When you are logged in, the **Last Heard** page shows new calls from you and to you as they happen. The page of your hotspot (click its ID on the Repeaters page) lists the calls through it.

## Next

[Send and read text messages](text-messages.md). Text messages find the other radio in the same way.
