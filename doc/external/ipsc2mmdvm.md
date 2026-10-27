# ipsc2mmdvm

Tested 2026-10-27 with ipsc2mmdvm 2bf4826 (github.com/USA-RedDragon/ipsc2mmdvm), against DMRHub 569c6e70. Only the MMDVM side was tested: ipsc2mmdvm logged in to DMRHub. The run had no Motorola repeater on the IPSC side.

## What it is

ipsc2mmdvm is a bridge for a Motorola repeater that speaks IP Site Connect (IPSC). The repeater connects to ipsc2mmdvm over Ethernet, with ipsc2mmdvm as its IPSC master. ipsc2mmdvm logs in to one or more DMR masters with the MMDVM network protocol, and sends the talkgroups to each master by rewrite rules in the form of DMRGateway.

## ipsc2mmdvm or the IPSC server of DMRHub

DMRHub has an IPSC server of its own (`dmr.ipsc.enabled`, UDP port 50000). With it, a Motorola repeater connects to DMRHub directly, with no other program. See [IPSC repeaters](../admin/ipsc.md).

| Use | When |
| --- | --- |
| The IPSC server of DMRHub | The repeater uses only DMRHub, and it can reach the IPSC port of DMRHub. |
| ipsc2mmdvm | The repeater must be on DMRHub and other networks at the same time, such as BrandMeister or TGIF. Or the repeater is on a local Ethernet port of a small Linux computer, and only that computer connects to DMRHub. |

With ipsc2mmdvm, DMRHub sees an MMDVM hotspot, not an IPSC repeater.

## What to set in DMRHub

Enroll the `radio-id` of the `mmdvm` entry at `<siteurl>/repeaters/new` with **Repeater Type** MMDVM, and copy the password. A 6-digit repeater ID needs a matching entry in the RadioID.net repeater list, unless the admin turned off those checks.

## Configuration

This `config.yaml` logged in to DMRHub. ipsc2mmdvm sets the address of the IPSC interface itself; the interface must exist.

```yaml
log-level: info
mmdvm:
  - name: DMRHub
    callsign: N0CALL
    radio-id: 300000108
    rx-freq: 438800000
    tx-freq: 433800000
    tx-power: 1
    color-code: 1
    latitude: 38.0
    longitude: -95.0
    height: 0
    location: Anytown
    description: ipsc2mmdvm test
    url: https://example.org
    slots: 3
    master-server: dmrhub.example.org:62031
    password: "<repeater password>"
    pass-all-tg: [1, 2]
    pass-all-pc: [1, 2]
ipsc:
  interface: eth0
  port: 50000
  ip: 10.10.250.1
  subnet-mask: 24
  auth:
    enabled: true
    key: "0123456789abcdef0123456789abcdef01234567"
```

- `pass-all-tg` and `pass-all-pc` send all group and private calls of both timeslots to this master, as `PassAllTG` and `PassAllPC` do in DMRGateway.
- `ipsc.auth.key` is the authentication key that you set in the repeater with CPS: up to 40 hex digits. It is not the DMRHub password.
- Without `ipsc.interface`, ipsc2mmdvm stops at start with `failed to load config: invalid IPSC interface provided`.

## Check that it works

The ipsc2mmdvm log shows the login:

```text
INF Connecting to MMDVM server network=DMRHub
INF Logged in network=DMRHub
```

On `<siteurl>/repeaters/300000108`, **Protocol** is MMDVM, **Package ID** is `MMDVM_MMDVM_HS_Dual_Hat`, **Software ID** is `20210921`, and the description is the one in the configuration.
