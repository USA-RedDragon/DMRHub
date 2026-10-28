# Pick your talkgroups

Your hotspot or repeater hears a talkgroup only when it carries that talkgroup on a timeslot. A timeslot carries talkgroups in two ways:

- **Static talkgroups** stay linked until you change them. A timeslot can have many static talkgroups.
- **One dynamic talkgroup** per timeslot. It links when someone transmits on a talkgroup through your hotspot or repeater, and it stays linked until someone transmits on another talkgroup on that timeslot or unlinks it.

To see the talkgroups of the network, go to **Talkgroups** (`<siteurl>/talkgroups`). Admins make the talkgroups. Click a talkgroup to see its details, its nets and its last calls.

## Link a talkgroup from the radio (dynamic)

1. Set your radio to the timeslot, usually TS2 on a simplex hotspot.
2. Make a group call to the talkgroup, for example TG 3100.

DMRHub links the talkgroup as the dynamic talkgroup of that timeslot. Your hotspot then hears the talkgroup. The talkgroup must exist on the network.

## Unlink the dynamic talkgroup

Make a voice call to TG 4000 on the timeslot. DMRHub removes the dynamic talkgroup of that timeslot. The static talkgroups stay.

## Set talkgroups on the Repeaters page

1. Log in and go to **Repeaters**.
2. Click **Edit** on the row of your hotspot or repeater.
3. In **TS1 Static TGs** and **TS2 Static TGs**, select the static talkgroups. These are multi-select lists: hold Ctrl (Command on a Mac) and click to select more than one.
4. In **TS1 Dynamic TG** and **TS2 Dynamic TG**, pick a dynamic talkgroup, or **0 - None**.
5. Click **Save**.

<!-- screenshot: repeaters-edit.png | the Repeaters page at /repeaters with one row in edit mode and TS2 static talkgroups selected -->

If an admin deletes a talkgroup while you edit, the save fails with "talkgroup N does not exist" and nothing changes.

If your hotspot or repeater sends static talkgroups when it connects (see the next section), they replace what you set here at its next login.

## Send static talkgroups from the hotspot (Options)

A hotspot or repeater can send its static talkgroups when it connects. Put them in the `Options` line of the `[DMR Network]` section of MMDVMHost, or of the network section in DMRGateway. DMRHub reads the same keys as FreeDMR, and the DMR+ keys too:

- `TS1=91,3100;TS2=9;` (or `TS1_STATIC=` and `TS2_STATIC=`) sets the static talkgroups of each timeslot.
- `TS1_1=91;TS1_2=3100;TS2_1=9;` (the DMR+ form) does the same, with up to nine talkgroups for each timeslot.

As in FreeDMR, options with static talkgroups set both timeslots. They replace the static talkgroups on the Repeaters page, take effect at once, and DMRHub saves them, so the Repeaters page shows what is active. If you change them on the Repeaters page later, the hotspot puts its own back the next time it connects.

- A timeslot that the options do not name loses its static talkgroups. `TS2=9;` alone leaves TS1 with none.
- An empty list, such as `TS2=;`, also removes all static talkgroups of that timeslot.
- DMRHub leaves out talkgroups that it does not have. If none of the talkgroups in a list exist, or the list has something that is not a talkgroup number, DMRHub leaves that timeslot as it is.
- Options with no static talkgroups, such as `DIAL=0;`, leave the static talkgroups as they are.

### Default dial

The default dial of a timeslot links a talkgroup as the dynamic talkgroup of that timeslot, as a call on it does. Like any other dynamic link, it changes when someone transmits on another talkgroup, and TG 4000 unlinks it.

- `DIAL=91;` (or `DIAL2=`, `DEFAULT_REFLECTOR=`, `default_dial_ts2=`, or the DMR+ `StartRef=`) links TG 91 on TS2. `DIAL1=` (or `default_dial_ts1=`) does the same on TS1.
- `DIAL=0;` or an empty value unlinks the dynamic talkgroup of that timeslot. So do the values that FreeDMR does not take as a default dial: 1 to 9, 9990 to 9999, and values above 999999.
- DMRHub ignores a talkgroup that it does not have.
- Without a default dial, the dynamic talkgroup stays as it is.

The hotspot sends its options each time it connects, so DMRHub links the default dial again at each login.

DMRHub has no timers, voice announcements or single mode, so it ignores the other FreeDMR and DMR+ options, such as `TIMER`, `RelinkTime`, `VOICE` and `SINGLE`.

## See what is linked

Click the ID of your hotspot on the Repeaters page. The **Talkgroup Subscriptions** card shows the static and dynamic talkgroups of each timeslot.

## Talkgroups you manage

An admin can make you an admin of a talkgroup. Then **Talkgroups**, then **Owned** (`<siteurl>/talkgroups/owned`) lists it. There you can click **Edit** to change its name and description, and pick its net control operators (NCOs). NCOs and talkgroup admins can start, stop and schedule the nets of the talkgroup. See [Run or join a net](nets.md).

## Restrict a talkgroup to approved repeaters

A talkgroup admin, or an admin of the network, can restrict a talkgroup. A restricted talkgroup links only to the repeaters that it approves, static or dynamic. Every talkgroup is open by default.

On a repeater that the talkgroup does not approve:

- A call to the talkgroup stops at the repeater. Last Heard shows the reason "is not approved for that talkgroup".
- The repeater does not get the calls of the talkgroup.
- A call does not link the talkgroup as the dynamic talkgroup.
- The Repeaters page refuses the talkgroup as a static or dynamic talkgroup, with "Talkgroup N links only to approved repeaters".
- The options of a hotspot (see above) leave the talkgroup out.

Until the talkgroup page shows this setting, use the API: `GET /api/v1/talkgroups/<id>/access` gives `mode` (`open` or `approved`) and `repeaters`, the approved repeaters. `PATCH` on the same address changes them, for example `{"mode": "approved", "repeaters": [311860, 311861]}`. A field that you leave out keeps its value.

