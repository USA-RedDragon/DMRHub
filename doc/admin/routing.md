# Routing settings

This page tells how DMRHub links talkgroups to the timeslots of repeaters and hotspots, and the settings that change it. DMRHub puts a change of these settings into effect at once on every replica, with no restart. Set them in the config file, in the environment or with flags. See [Configuration](configuration.md).

| Setting | What it does | Default | Configuration key |
| ------- | ------------ | ------- | ----------------- |
| Dynamic links off | When `true`, a call on a talkgroup and the default dial of a hotspot link no dynamic talkgroup. See [Dynamic talkgroups](#dynamic-talkgroups). | `false` | `dmr.routing.disable-dynamic-links` |
| Dynamic talkgroup timeout | The time with no transmission from a repeater on its dynamic talkgroup after which DMRHub unlinks the talkgroup from that timeslot, such as `15m`. `0s` never unlinks it. | `15m` | `dmr.routing.dynamic-timeout` |

## Dynamic talkgroups

A group voice call from a repeater or hotspot links its talkgroup as the dynamic talkgroup of the timeslot, as does the default dial in the options of a hotspot. Each timeslot has one dynamic talkgroup. A call on another talkgroup replaces it, and a voice call to talkgroup 4000 unlinks it.

Each voice call from the repeater on its dynamic talkgroup starts the timeout again. Calls from other repeaters on the talkgroup do not. When the timeout passes with no such call, DMRHub unlinks the talkgroup from that timeslot. The static talkgroups stay.

- The replica that serves the repeater counts the timeout. When the repeater connects again, or moves to another replica, the count starts again from that time.
- A dynamic talkgroup that a user or an admin picks on the **Repeaters** page also times out, counted from the time that DMRHub sees it.
- A change of the timeout applies to the links that exist. With `0s`, no link times out.

With dynamic links off, calls still reach the repeaters that carry the talkgroup as a static talkgroup or as a dynamic talkgroup that was linked before. A user or an admin can still pick a dynamic talkgroup on the **Repeaters** page, and talkgroup 4000 still unlinks.
