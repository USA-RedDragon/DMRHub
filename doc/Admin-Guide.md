# Admin's Guide

This guide is for users of DMRHub who have been granted administrative permissions.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

If you are the server operator looking to deploy DMRHub, see the [Deployment Guide](Deployment-Guide.md) and [Configuration Guide](Configuration.md).

## The Administrator Role

The administrator role exists to share the burden of the manual user approval process and other user support actions. The first admin user is created during the [setup wizard](Configuration.md#setup-wizard-recommended) on initial deployment.

## Administrator Capabilities

DMRHub administrators can:

- List, edit, and delete any repeater
- Create and edit talkgroups
- Create, delete, and assign admins and net control operators (NCOs) to talkgroups
- List, suspend, approve, or edit users
- View last heard for a specific user or repeater

## The Navigation Bar

When logged in as an administrator, the navigation bar displays an **Admin** dropdown with links to:

- **Talkgroups** — manage all talkgroups
- **Repeaters** — manage all repeaters
- **Users** — manage user accounts
- **User Approvals** — process pending registrations
- **Messages**: search all stored text and data messages (see [Messages](#messages))
- **Setup** — update configuration settings

## Talkgroups

The admin talkgroup page is similar to the main talkgroup list, but all talkgroups' names, descriptions, admins, and NCOs are editable and all talkgroups can be deleted.

### Creating Talkgroups

Click **Add New Talkgroup** in the table header. Name and Description are required; Talkgroup Administrators and NCOs are optional.

## Repeaters

The admin repeaters page lists all repeaters on the system, not just those owned by the current user.

## Users

The users page allows administrators to edit and delete users. Current editable actions include suspending and unsuspending users.

## User Approvals

This list shows users who have registered and are awaiting access. These users have already been validated as having a valid DMR ID and callsign combination.

> **Note:** RadioID.net validation confirms the callsign/DMR ID pair exists in the database, but does not verify the registrant actually owns that callsign. Use your judgment when approving users.

Click **Approve** to grant the user access to sign in and use DMRHub.

## Setup

The setup page allows administrators to edit the DMRHub configuration. All configuration options are available here. In deployments with read-only file-based configuration (e.g., Docker), this page is view-only and shows the current effective configuration.

## Messages

DMRHub routes text messages (SMS) and other data messages between radios. For what users see, see [Messages and SMS Storage](messages.md).

### What the hub stores

DMRHub decodes and stores every text message and data message that it routes. It does this for all users, whatever their settings. The hub owner is responsible for the traffic that the hub routes, and uses these records to make sure that the traffic obeys the amateur radio rules.

For each message, DMRHub stores:

- the time, the source radio ID, and the destination radio ID or talkgroup;
- the repeater, the timeslot, and the network (MMDVM, IPSC or OpenBridge) that the message came from;
- the format and the text, when the message is text;
- if the message was confirmed, and the reply (ACK, NACK or SACK);
- the raw bytes: the data header, then the reassembled user data.

DMRHub reads these text formats:

| Format          | Description                                                  |
| --------------- | ------------------------------------------------------------ |
| ETSI IP         | Text in an IP and UDP packet                                 |
| Motorola TMS    | Motorola Text Messaging Service                              |
| AnyTone         | AnyTone text messages                                        |
| Hytera          | Hytera text messages                                         |
| Defined Data    | Text sent as ETSI defined short data                         |
| Data (not text) | A data message that is not text, for example location data   |

### Search all messages

Admins can search every stored message, in every format, from every user. Go to **Admin**, then **Messages** (`<siteurl>/admin/messages`). The search has no 250-message limit and does not use the SMS Storage setting of the users.

You can search by:

- **From ID**: the source radio ID;
- **To ID or talkgroup**: the destination radio ID or talkgroup;
- **Format**;
- **From date** and **To date**: the days in your local time zone, both included;
- **Text contains**: a part of the text, in upper or lower case.

Click a row to see the details of the message and its raw bytes.

### What users can see

A user sees a message only when all of these are true:

- SMS Storage is on for the user.
- The message arrived after the user last turned on SMS Storage. When a user turns SMS Storage off and on again, the start time moves to the new time.
- The message is to (inbox) or from (outbox) a radio ID that the user owns. For an added ID, the message also arrived after DMRHub verified the ID.
- The message is one of the 250 newest of its box.

The counts and statistics on the Messages page use the same rules, so they never show more than 250 messages in a box, or messages from before the start time.

### Retention

By default, DMRHub keeps all records forever. To delete old records, go to **Admin**, then **Setup**, and find the **Retention** card in **DMR Settings**. Set each of these:

| Setting       | What it deletes                                          | Configuration key           |
| ------------- | -------------------------------------------------------- | --------------------------- |
| Messages      | Text and data messages, with their raw bytes             | `dmr.retention.messages`    |
| Transmissions | Last heard entries for voice, data and control traffic   | `dmr.retention.transmissions` |
| Raw bursts    | The raw DMR bursts of each transmission                  | `dmr.retention.bursts`      |

Each value is a number of days, from 0 to 36500. 0 keeps the records forever. The Setup page offers Keep forever, 30 days, 90 days, 1 year, 2 years and 5 years. A different value from the configuration file shows as a number of days.

Retention also deletes records from the admin search. Set a period that your local rules allow.
