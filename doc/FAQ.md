# Frequently Asked Questions

## About the Project

### What is DMRHub?

DMRHub is a DMR master system that can be deployed with minimal fuss. It is an open source, easy-to-use alternative to existing networks like BrandMeister and TGIF, designed for use in local communication environments such as over an [AREDN mesh](https://www.arednmesh.org/) network.

### What systems is it compatible with?

DMRHub supports DMR layer 2 packets sent over UDP using the MMDVM protocol. This is the method that MMDVM hotspots typically use with networks like BrandMeister or TGIF. It supports private and group calls, a Parrot (TG 9990), and experimental OpenBridge peering. I have another project for IPSC repeaters to convert the packets to MMDVM format <https://github.com/USA-RedDragon/ipsc2mmdvm>. This requires a spare NIC attached to the repeater directly, which may not be possible in all deployments.

### Can you make it work with my repeater technology?

Probably, depending on the situation. If you have access to the hardware or captured DMR data to work with, support can be investigated. Well-defined documentation of the protocol also helps. The goal is to support the most common DMR deployments.

## Software

### Why is the binary so big?

DMRHub is written in Go and statically compiles its binaries, meaning no external libraries or dependencies are needed at runtime. It also embeds the [RadioID.net](https://radioid.net/) DMR radio ID and repeater ID databases. Compressed, these account for about 4 MB of the binary. On startup, they are decompressed into memory and take up about 40 MB.

### What databases are supported?

DMRHub supports three database backends:

- **SQLite** (default) — no external database needed, perfect for single-server deployments
- **PostgreSQL** — for production deployments requiring a dedicated database server
- **MySQL** — alternative relational database option

### Is Redis required?

No. Redis is optional and disabled by default. When enabled, Redis is used for the pub/sub backend, which allows scaling across multiple DMRHub instances. For single-server deployments, the in-memory pub/sub backend works fine.

### How does initial setup work?

When DMRHub starts and the configuration is missing or invalid, it automatically launches a **setup wizard** in your web browser. The wizard walks you through configuring the application and creating your first admin user. No manual configuration file editing is required for basic setups. See the [Configuration Guide](Configuration.md) for details.
