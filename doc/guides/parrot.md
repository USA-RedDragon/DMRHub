# Test with the parrot

The parrot records your call and plays it back to you. Use it to check your audio and your hotspot before you talk to other hams.

The parrot is a plugin. It works only when your admin installed it. It usually answers on ID 9990. Ask your admin if 9990 does not answer.

## Call the parrot

1. Set your radio to the timeslot that your hotspot uses.
2. Make a call to 9990. A private call and a group call both work.
3. Talk for a few seconds, then release the PTT.
4. About 3 seconds after your call ends, the parrot plays your call back to you as a private call from 9990.

<!-- screenshot: lastheard-parrot.png | the Last Heard page at /lastheard with a call from N0CALL to 9990 and the reply from 9990 -->

The parrot keeps at most 3 minutes of one call.

## If you hear nothing

- Check on the Repeaters page that your hotspot connected. **Last Ping** must show a recent time.
- Check the **Last Heard** page. If your call to 9990 is not there, DMRHub did not get it. Check the hotspot configuration.
- If your call is there but no reply comes, check that your radio receives private calls from 9990. Some radios need a contact or an RX group list entry, or a "monitor private calls" setting.
- Ask your admin whether the parrot plugin runs on this network.
