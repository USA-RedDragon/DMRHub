# Ask 5000 for your status

The status service tells you which talkgroups your hotspot or repeater carries on the timeslot that you use. Call it or send it a text message on ID 5000.

The status service is a plugin. It comes with DMRHub and is on by default. Ask your admin if 5000 does not answer.

## Call 5000

1. Set your radio to the timeslot that you want to know about.
2. Make a private call to 5000. Key up for a moment, then release the PTT.
3. About 1 second after your call ends, 5000 calls you back with a private call and says the status of that timeslot, for example: "Timeslot two. Linked: talkgroup three one zero zero, static. Dynamic: none."

The status says:

- **Timeslot**: the timeslot that you called on.
- **Linked**: each static talkgroup of that timeslot, which the owner of the hotspot or repeater set, followed by "static". "None" when there is none.
- **Dynamic**: the talkgroup that a radio linked last by keying up on it. "None" when there is none.

5000 speaks in the voice that you picked under [Spoken prompts](account.md#spoken-prompts) in your settings, or in the voice of the network, whether or not the admin turned spoken refusals on. A Codec2 hotspot gets the Codec2 copy of the prompts.

## Send a text to 5000

1. On your radio, write a message. The text does not matter, for example `STATUS`.
2. Send it to 5000 as a private message.
3. 5000 answers with a text, for example `Timeslot 2. Linked: talkgroup 3100, static. Dynamic: none.`

5000 answers in the exact format of your message: the same text encoding, header type and rate, through the same hotspot or repeater and timeslot. The wrong text format can lock up a radio, so 5000 sends no answer in a format that your radio did not send. When the answer is too long for the format, 5000 sends a short one, such as `TS2 S:3100 D:-`.

5000 does not answer:

- A text that came through a hotspot or repeater that is not yours and that the admin did not mark as trusted, because another hotspot can send with your radio ID.
- A text in the UDT format, as DMRHub cannot yet send a UDT text.
- A text to a talkgroup.

## If nothing comes back

- Check the **Last Heard** page. It shows your call or text to 5000. If it is not there, DMRHub did not get it.
- If your call is there but no reply comes, check that your radio receives private calls from 5000. Some radios need a contact or an RX group list entry, or a "monitor private calls" setting.
- Ask your admin whether the status plugin is on, on the Plugins page.
