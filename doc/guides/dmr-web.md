# Use dmr-web, the DMR radio in your browser

dmr-web is a DMR radio that runs in a web browser, on a computer or a phone. You can talk on talkgroups, make private calls, send text messages, and share your position, with nothing to install. dmr-web joins the network as a hotspot of your account, so it follows the same rules as any other hotspot.

> **This is a real DMR radio.** What you send goes on the air under your callsign and DMR ID, and your license rules apply, such as identifying your station. Nothing is encrypted. dmr-web shows this notice before you first use it, and you click **I understand**.

## What you need

- A DMRHub account. See [Create an account](create-account.md).
- A network that accepts browser radios. The admin must turn on WebTransport on DMRHub. Ask your admin for the address of dmr-web on your network.
- A browser with WebTransport: Chrome 97, Edge 98, Firefox 114, Safari 26.4, or newer. Some work, school and hotel networks block it. If dmr-web shows **WEBTRANSPORT FAILED**, try another network or mobile data.
- A microphone, to talk. Without one you can still listen.

## Log in with your DMRHub account

dmr-web does not ask for your DMRHub password. It sends you to the DMRHub web site, and you allow it there.

1. Open dmr-web.
2. In **Hub**, type the address of your DMRHub network, for example `dmrhub.example.org`.
3. Click **Log in with DMRHub**.
4. DMRHub opens. Log in, if you are not logged in already.
5. DMRHub shows **Allow this app to use your DMRHub account?** with the address of dmr-web and what it can do:
   - See the talkgroups of this network
   - See last heard for your radio IDs and your hotspots
   - Read your text messages, and get new ones as they arrive
   - See your hotspots
   - Add hotspots to your account and see their passwords
   - Change the talkgroups of your hotspots
6. Click **Allow**. DMRHub sends you back to dmr-web.

<!-- screenshot: dmr-web-login.png | the dmr-web start page with the Hub field and the Log in with DMRHub button -->

dmr-web keeps your login on this device. To end it, click **Log out** in dmr-web **Settings**, **Connection**, or click **Remove** next to dmr-web in the **Apps** card on DMRHub **Settings**. See [Manage your account](account.md#apps).

## Pick a hotspot

dmr-web then shows **Welcome**, with your callsign, and the hotspots of your account.

- **Register a new hotspot**: dmr-web adds a hotspot to your account. The ID is your DMR ID plus two digits that you pick, for example 3100001 + 07 = 310000107. Give it an optional **Name** and click **Register**. DMRHub makes a password and shows it once. dmr-web saves it on this device. Click **Copy** if you also want to use this ID in another place, then click **Connect now**.
- **Pick a hotspot from the list**: a hotspot marked **READY** has its password saved on this device. For another hotspot, type the **Hotspot password** that DMRHub showed when you enrolled it. dmr-web saves it on this device.
- **Enter ID and password by hand**: type a hotspot ID and its password.

Use a hotspot ID for dmr-web that no other hotspot uses at the same time.

Check **Callsign**, then click **Connect**.

<!-- screenshot: dmr-web-hotspots.png | the dmr-web Welcome page with two hotspots, one marked READY -->

The new hotspot also shows on the Repeaters page of DMRHub.

## Talk

1. Pick the **SLOT** (timeslot) and turn the **TALKGROUP** knob to a talkgroup. The first two talkgroups of the slot are also under **PRESETS**.
2. To call a talkgroup or radio that is not on the list, tap the talkgroup display and type the number on the keypad. Pick **Talkgroup** or **Private** for the call type.
3. Hold the PTT button, or hold the space bar, and talk. Release it to stop.

The first time you talk, the browser asks for the microphone. Click **Allow**.

To tap once to talk and tap again to stop, set the push to talk mode to **Tap to toggle** (**Toggle** on a phone) in **Settings**, **Audio**. You can also change the keyboard key there.

When someone calls you privately, the PTT answers that radio for a few seconds, then goes back to the talkgroup.

TS2 starts with the parrot, 9990, on the list. Call it to test your audio. See [Test with the parrot](parrot.md).

## Your talkgroup list

Go to **Settings**, **Talkgroups** to add, name, reorder and remove the talkgroups of each slot. The knob steps through the list in this order.

Tap **STATIC** on a talkgroup to make it a static talkgroup of your hotspot. When you are logged in, dmr-web saves the static talkgroups to your hotspot on DMRHub, so the Repeaters page shows them too. When you pick a hotspot that already has static talkgroups on DMRHub, dmr-web adds them to its list.

## Voice codec

dmr-web transmits Codec2 unless you load an AMBE module. In **Settings**, **Vocoder**, pick **Auto** (AMBE when a module is loaded), **Codec2** or **AMBE**. dmr-web plays received calls in either codec.

For other radios to hear your Codec2 calls, the network must convert them to AMBE. Ask your admin if the network has transcoding. See [Use Codec2 radios](voice-codec.md).

## Text messages

1. Open **Messages**.
2. Click **+** to start a new message.
3. In **To**, type a callsign, a radio ID or a talkgroup.
4. Write the message and send it.

dmr-web shows the messages that it sends and receives while it is on. When SMS Storage is on for your account, it also loads the messages of your inbox and outbox from DMRHub. See [Send and read text messages](text-messages.md).

## Share your location

1. Open **Map**.
2. Turn on **Share my location**, and allow the browser to use your location.
3. Pick the **APRS symbol**, or click **Send my position once**.

dmr-web sends your position to the location server of the network. DMRHub keeps it only when position tracking is on for your radio ID. To turn it on, go to **Positions** on DMRHub and click **Settings** next to your radio ID. See [Share the position of your radio](positions.md).

In **Settings**, **Connection**, **ARS registration** tells the location and text servers that this radio is on, so they can send it texts and location requests.

## If dmr-web does not connect

| dmr-web shows | What to do |
| --- | --- |
| NO WEBTRANSPORT | The browser does not have WebTransport. Use a newer browser. |
| WEBTRANSPORT FAILED | The network you are on, or the hub, blocks the connection. Try another network or mobile data. |
| The hub may not be a DMRHub, or may not accept browser radios | Check the hub address. Ask your admin whether WebTransport is on. |
| Your DMRHub login ended. Log in again. | The login expired or you removed dmr-web on DMRHub. Log in again. |
| Hotspot IDs are 7 or 9 digits and start with your radio ID | Fix the hotspot ID. |
