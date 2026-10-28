# Transmission kinds

This page lists the `kind` and the `sub_kind` of each Last Heard entry, and the reasons why a target could not carry a burst. It is a reference for admins and for apps that read the API or the websockets.

## Kinds

Each Last Heard entry has a `kind` and a `sub_kind`. The API and the websocket give them in these fields, and the values stay the same between releases. A sub kind that names a CSBK opcode, a SAP, a port, a USBD service type or a text format is the dmrgo name of that value in lower case, without "PDU", with `_` between the words. For example, the BS outbound activation CSBK is `bs_outbound_activation_or_td_grant_mi`.

| `kind`    | `sub_kind`                                                                                                                                                                             |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `voice`   | empty, or `encrypted` when the call had a PI header                                                                                                                                    |
| `data`    | `sms` for a text message, `response` for an ACK, NACK or SACK, the port name of an IP packet (`lrrp`, `ars`, `tms`, `text`, `location` and others), `ip` for another IP packet, or the SAP name (`short_data`, `proprietary_packet_data` and others) |
| `udt`     | empty                                                                                                                                                                                  |
| `csbk`    | `preamble` when no data followed it, `radio_check`, `radio_check_answer`, `call_alert`, `call_alert_ack`, another standard opcode name, or a Capacity Plus opcode name such as `capacity_plus_site_status` |
| `mbc`     | the opcode name, such as `c_bcast`                                                                                                                                                    |
| `usbd`    | the service type name, such as `location_information_protocol`                                                                                                                       |
| `idle`    | empty                                                                                                                                                                                  |
| `alias`   | empty, or `partial` when only part of the alias arrived                                                                                                                                |
| `gps`     | empty, or `partial` for a short DMRG that has no coordinates                                                                                                                          |
| `unknown` | `bad_crc`, `undecodable`, `reserved`, `frame_type_3`, or `csbk` for a CSBK opcode that DMRHub does not know                                                                         |

The `data_format` of a text message is `tms`, `ip`, `anytone`, `defined_data`, `hytera` or `unknown`.

## Undeliverable bursts

The `undeliverable` list has one item for each target that could not take a burst of the entry. Each item has the stored `sentence`, for example `IPSC repeater 310001 was busy on that slot.`, the `target` server (`ipsc`, `mmdvm` or `openbridge`), the `repeater_id`, the repeater `callsign`, and the `reason` code. The reason codes are `invalid_data_type`, `no_ipsc_form`, `frame_type_3`, `fec_uncorrectable`, `slot_type`, `stream_id_range`, `build_failed`, `encode_failed`, `data_in_voice`, `undecodable`, `slot_busy`, `slot_locked`, `talkgroup_refused` and `talkgroup_restricted`. The last three come from the rules of the repeater that sent the burst (see [Set the rules of your repeater](../guides/repeater-rules.md)) and from restricted talkgroups (see [Pick your talkgroups](../guides/talkgroups.md#restrict-a-talkgroup-to-approved-repeaters)). The `reason` label of the `dmr_undeliverable_bursts_total` metric uses the same codes.

Each viewer gets only the items they may see. The caller and an admin get every item, and `undeliverable_scope` is `all`. The owner of a repeater gets only the items for their own repeaters, and `undeliverable_scope` is `own`. Anyone else, and a viewer who is not logged in, gets no items, and `undeliverable_scope` is empty. This applies to the REST API and to the websockets.

