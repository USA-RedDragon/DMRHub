# Transcoding between Codec2 and AMBE

Most DMR radios use the AMBE voice codec. Some radios and apps, such as the [DMRHub Browser Radio](https://radio.dmrhub.net), use Codec2. DMRHub can convert each call between the two, so that both kinds of radio hear each other. For what users set, see [Use Codec2 radios](../guides/voice-codec.md).

DMRHub has the Codec2 codec built in. For AMBE it needs one or more AMBE servers that speak the AMBEserver protocol, usually on UDP port 2460. Without an AMBE server, transcoding is off, and each call goes out in its own codec only.

## Add AMBE servers

1. Go to **Admin**, then **Setup**, and open the **Transcoding** tab.
2. Click **Add a server**.
3. In the address box, enter the host and the UDP port, for example `ambeserver.example.org:2460`.
4. In **Calls at once**, enter how many calls the server can convert at the same time. Leave it empty when the server has no limit. Use 1 for a server with one vocoder chip, which answers only its last client.
5. Click **Save**.

The configuration key is `dmr.transcoding.ambe-servers`, a list of `address` and `calls`:

```yaml
dmr:
  transcoding:
    ambe-servers:
      - address: ambeserver.example.org:2460
        calls: 1
```

The order of the list is the order in which a call tries the servers. Each converted call holds one server for the whole call. DMRHub converts each call once, only for the targets that need the other codec.

Each server must accept the setting that makes it report the decoder status of each frame (`PKT_SPCHFMT`). A server that refuses this setting converts no call.

<!-- screenshot: admin-setup-transcoding.png | /admin/setup, the Transcoding card with one AMBE server ambeserver.example.org:2460, calls at once 1, and its Status -->

## Check the servers

On the **Transcoding** tab of the **Setup** page, the status next to each saved server shows:

| Status | What it means |
| ------ | ------------- |
| Checked after you save. | The server is new. DMRHub checks it after you click **Save**. |
| Answered N seconds ago. N calls now. | The server answers. It converts that many calls now. |
| The AMBE server did not answer. | DMRHub cannot reach the server, or the server does not answer. |

## When the servers are busy

When every server is busy, a call passes through in its own codec, and the targets that use the other codec do not get it. DMRHub logs `Every AMBE server is busy, a call is not converted`. The Last Heard page shows "Not converted: every AMBE server was busy" in the **Codec** column of that call.

These metrics show the work of the transcoder (see [Ports](install.md#ports) for the metrics server):

| Metric | What it counts |
| ------ | -------------- |
| `dmr_transcode_frames_total` | The vocoder frames that the transcoder decoded, by codec and result |
| `dmr_transcode_missed_seconds_total` | The seconds of call audio that DMRHub could not convert, which went out as silence |

To carry more converted calls at the same time, add AMBE servers, or raise **Calls at once** on servers that can do more.
