# Talkgroups

This page tells how an admin makes, edits and deletes talkgroups. For how users pick talkgroups for their hotspots, see [Talkgroups](../guides/talkgroups.md).

A group call goes only to a talkgroup that exists. DMRHub drops a group call to an ID that is not a talkgroup, unless a plugin owns that ID (see [How the hub routes plugin calls](../plugins.md#how-the-hub-routes-plugin-calls)). A voice call to talkgroup 4000 unlinks the dynamic talkgroup of the timeslot, whether or not a talkgroup 4000 exists.

## Make a talkgroup

1. Go to **Admin**, then **Talkgroups** (`<siteurl>/admin/talkgroups`).
2. Click **Add New Talkgroup**.
3. Fill in the form:

   | Field | What to enter |
   | ----- | ------------- |
   | **Talkgroup ID** | The number that radios call. It must be a number that no other talkgroup has. |
   | **Name** | Up to 20 characters. |
   | **Description** | Up to 240 characters. |
   | **Admins** | Optional. The users who can edit the talkgroup and pick its NCOs. |
   | **Net Control Operators** | Optional. The users who can run nets on the talkgroup. |

4. Click **Save**.

<!-- screenshot: admin-new-talkgroup.png | /admin/talkgroups/new filled in with ID 91, name "Doc TG", one admin N0CALL -->

## Edit a talkgroup

1. On the **Talkgroups** page, click **Edit** in the row of the talkgroup.
2. Change the **Name**, the **Description**, the **Admins** or the **Net Control Operators**.
3. Click **Save**, or **Cancel** to keep the old values.

The admins of a talkgroup can also edit it on their **Owned** talkgroups page, and pick its NCOs.

## Delete a talkgroup

On the **Talkgroups** page, click **Delete** in the row of the talkgroup, then click **OK** in the browser message.

<!-- screenshot: admin-talkgroups.png | /admin/talkgroups with talkgroup 91 in the table, showing the Edit and Delete buttons and the Admins and Net Control Operators columns -->
