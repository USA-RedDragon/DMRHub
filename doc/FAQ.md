# Frequently asked questions

## Using a network

### What is DMRHub?

DMRHub is a DMR network server. It is an open source alternative to networks such as BrandMeister and TGIF, made for local networks. See [Get started](getting-started.md).

### Is my DMRHub account good on BrandMeister, TGIF or another DMRHub network?

No. Each DMRHub network has its own accounts, talkgroups and hotspot passwords.

### Which hotspots and repeaters work?

- Hotspots and repeaters that use the MMDVM network protocol, such as MMDVMHost, DMRGateway and Pi-Star. See [Connect a hotspot](guides/connect-hotspot.md).
- Repeaters that follow the published Homebrew repeater specification. See [Link a repeater](guides/link-repeater.md).
- Motorola repeaters over IPSC, when the admin turned on IPSC. See [Link a repeater](guides/link-repeater.md#motorola-ipsc-repeaters).
- DroidStar, as an MMDVM client.
- dmr-web, in a browser, when the admin turned on WebTransport. See [Use dmr-web](guides/dmr-web.md).

### Which talkgroups are special?

- **4000** unlinks the dynamic talkgroup of the timeslot you call it on. See [Pick your talkgroups](guides/talkgroups.md#unlink-the-dynamic-talkgroup).
- **9990** is the parrot, when the admin installed the parrot plugin. See [Test with the parrot](guides/parrot.md).
- **5050 to 5059** are the location server, when the admin installed the location server plugin. See the [Positions reference](positions.md#location-server-ids).

### Why does my private call not reach the other ham?

DMRHub sends a private call to the hotspot or repeater where it last heard the other radio. With the default settings, the other radio needs no account, but a radio ID that a user owns counts only on a hotspot or repeater of that user or a trusted one. DMRHub drops the call when it never heard the other radio. The admin can change these rules and set a time limit. See [Make a private call](guides/private-calls.md#how-dmrhub-finds-the-other-radio).

### I lost the password of my hotspot. Can I see it again?

No. DMRHub shows it only once. Delete the hotspot on the Repeaters page and enroll it again.

### Who can see my text messages and positions?

Your text messages show on the web site only when you turn on SMS Storage, and only you and the admins see them. Your positions show only to you, until you turn on **Share my positions**. See the [Messages reference](messages.md) and the [Positions reference](positions.md#who-sees-a-position).

### Can my repeater technology be added?

Probably, depending on the case. Access to the hardware, or captured DMR data, makes support possible to investigate. Good documentation of the protocol also helps.

## Running a network

### Why is the binary so big?

DMRHub is a static Go binary, so it needs no external libraries at run time. It also has the [RadioID.net](https://radioid.net/) DMR ID and repeater ID databases built in. Compressed, they take about 5 MB of the binary. DMRHub decompresses them into memory when it starts, and then downloads new copies when it starts and once a day.

### Which databases does DMRHub support?

- **SQLite** (default): no external database. Good for one server.
- **PostgreSQL** (16): a dedicated database server.
- **MySQL** (8.4) and **MariaDB** (12.3).

See [Supported Databases](admin/configuration.md#supported-databases) and the MySQL settings in [Configuration](admin/configuration.md#mysql-and-mariadb).

### Is Redis required?

No. Redis is optional and off by default. Without it, DMRHub uses an in-memory pub/sub, which is enough for one server. With Redis, more than one DMRHub instance can share the work.

### How does the first setup work?

When DMRHub starts and the configuration is not valid, no admin exists yet, or the database cannot be reached, it starts a setup wizard in your web browser. The wizard fixes the database settings if needed, saves the other settings in the database and makes the first admin user. DMRHub makes its own secret and password salt when you set none. See [Configuration](admin/configuration.md).
