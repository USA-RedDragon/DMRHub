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
