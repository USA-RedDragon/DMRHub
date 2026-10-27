# Private call routing

A private call, a private text message and other private data go to one radio. This page tells how DMRHub finds that radio, and the settings that change it. For what users see, see [Make a private call](../guides/private-calls.md).

## How DMRHub finds the radio

DMRHub keeps a record of where each radio was last heard: the repeater or hotspot, and the time. Each transmission of a radio on a repeater or hotspot that is logged in to this network updates the record, and the newest sighting wins. The record lasts: it does not expire, and it stays when the radio is quiet. Transmissions that come from OpenBridge peers and from plugins do not count.

When a private call starts, DMRHub sends it to the newest place in the record that the settings allow. All bursts of one call go to the same place, even when the radio is heard elsewhere during the call. A call to the 6-digit ID of a repeater goes to that repeater, and these settings do not change that.

A radio ID that a user owns is the DMR ID of an account, or a radio ID that a user added on the Settings page.

## Settings

DMRHub has one set of settings for private voice calls, and one for private text messages, data and CSBK, such as radio checks and call alerts. Each data setting that you leave empty takes the value of the voice setting.

| Setting | What it does | Default | Configuration key |
| ------- | ------------ | ------- | ----------------- |
| Reach | The radios that private calls reach. See [Reach](#reach). | `hybrid` | `dmr.private-calls.voice.reach` |
| Unknown radio | What DMRHub does with a call to a radio that the reach allows, but that DMRHub did not hear, or did not hear within the max age: `drop` drops the call and logs it, and `flood` sends it to every repeater and hotspot except the one it came from. | `drop` | `dmr.private-calls.voice.unknown` |
| Max age | The longest time since a radio was last heard on a repeater that calls still go there, such as `24h`. `0s` is no limit. | `0s` | `dmr.private-calls.voice.max-age` |
| Data reach | The reach for text messages and data. Empty takes the voice reach. | empty | `dmr.private-calls.data.reach` |
| Data unknown radio | The unknown radio setting for text messages and data. Empty takes the voice setting. | empty | `dmr.private-calls.data.unknown` |
| Data max age | The max age for text messages and data. `0s` takes the voice max age. A negative value, such as `-1s`, is no limit, also when the voice max age has a limit. | `0s` | `dmr.private-calls.data.max-age` |

The Setup page does not show these settings. Set them in the config file, in the environment, such as `DMR_PRIVATE_CALLS_VOICE_REACH=members`, or with flags. See [Configuration](configuration.md). For example:

```yaml
dmr:
  private-calls:
    voice:
      reach: hybrid
      unknown: drop
      max-age: 24h
    data:
      max-age: -1s
```

With this file, voice calls go only to radios heard in the last 24 hours, and text messages go to where the radio was last heard, however long ago.

## Reach

| Reach | Radios that private calls reach |
| ----- | ------------------------------- |
| `hybrid` | Any radio heard on a repeater or hotspot of this network. A radio ID that a user owns counts only where that user owns the repeater or hotspot, or where an admin marked it trusted. |
| `members` | Only the radio IDs that users own, on the repeaters and hotspots of their owner or trusted ones. DMRHub drops calls to other radios, also with `flood`. |
| `anyone` | Any radio, wherever it was last heard. DMRHub does not check who owns the radio or the repeater. |

The ownership check of `hybrid` and `members` stops spoofing. For example, user 3000001 owns hotspot 300001, and user 3000002 owns hotspot 300002. When hotspot 300002 sends a transmission with the source ID 3000001, calls to 3000001 still go to hotspot 300001 in `hybrid` and `members`. In `anyone`, they go to hotspot 300002. DMRHub logs each sighting of an owned radio ID on a repeater that its owner does not own and no admin trusts.

To let a shared repeater move the calls of the radios that users own, mark the repeater trusted. See [Repeaters](repeaters.md).

## Where the record comes from

DMRHub keeps the record in the KV store (Redis, when Redis is on), under the key `dmrhub:lastheard:<radio ID>`. Routing never waits on Redis to write the record. When Redis is slow or stopped, DMRHub queues the records and writes them when Redis is back. Until then, it uses where this replica heard the radio in the last 30 seconds. Without Redis, the record is in the memory of the replica and is lost at a restart.

For the DMR ID of an account with no record, DMRHub also looks at the last call of that ID on a repeater or hotspot that the user owns or an admin trusts. The max age applies to that call too.
