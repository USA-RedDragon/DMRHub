# Users

This page tells how to approve new users, and how to edit, suspend and delete users. You must be an admin. Some actions need the super admin (see [Admins and the super admin](overview.md#admins-and-the-super-admin)).

## Approve new users

When a ham registers at `<siteurl>/register`, DMRHub checks that RadioID.net lists the DMR ID under the callsign (see [RadioID checks](#radioid-checks)). A new password must have at least 8 characters. This applies when a user registers, changes their password, or uses a reset link, and when an admin sets a password. A password set before this rule still works until the user changes it. If you set the HIBP API key (`hibp-api-key`), DMRHub also refuses passwords that are in the Have I Been Pwned list.

Then the new user waits for an admin, unless `auto-approve-users` is on. With `auto-approve-users` on, the new user can log in at once.

To approve a user:

1. Go to **Admin**, then **User Approvals** (`<siteurl>/admin/users/approval`).
2. Find the user, and click **Approve** in the **Approve?** column.
3. The browser shows the message "Are you sure you want to approve this user?". Click **OK**. This message has no **Cancel** button: DMRHub approves the user when it closes.

The RadioID.net check shows that the DMR ID and the callsign go together. It does not show that the person who registered holds that callsign. Use your own judgment before you approve a user.

<!-- screenshot: admin-user-approvals.png | /admin/users/approval with one user waiting (N0CALL, 3000001), the Approve button visible -->

## Who can register

`registration` sets who can register:

- `open`: anyone. This is the default.
- `invite`: only a person with an invite from an admin. A user who registers with an invite can log in at once.
- `closed`: nobody. The register page refuses each registration.

The first admin can register in any mode.

### Invite a user

When `registration` is `invite`, an admin makes an invite with `POST /api/v1/admin/invites`. The answer has the invite link (`<siteurl>/register#invite=<code>`) and its code. DMRHub shows the code only this one time. Send the link to the person. The link works once, for 7 days.

`GET /api/v1/admin/invites` lists each invite, who made it, and who used it. `DELETE /api/v1/admin/invites/<id>` stops an unused invite.

## Edit a user

1. Go to **Admin**, then **Users** (`<siteurl>/admin/users`).
2. Click **Edit** in the row of the user.
3. Change the **Callsign** or the **Username**, then click **Save Changes**. Click **Cancel** to keep the old values.

DMRHub checks a new callsign against RadioID.net when the callsign check is on. When RadioID.net lists another callsign for the DMR ID, DMRHub shows that callsign. Click **Save <callsign> anyway** to keep yours. The RadioID drift check then skips the user. DMRHub refuses a callsign that another user has.

Users can change their own username and callsign in the **Account** card on **Settings**. A user can only set the callsign that RadioID.net lists for their DMR ID.

Usernames are the same in any case, so a user can log in as `alice` or `Alice`, and two users cannot have usernames that differ only by case. A user can log in with their callsign in any case too. A username cannot be the callsign of another user, in any case, and a callsign cannot be the username of another user, because a login by a username comes before a login by a callsign. A user can have their own callsign as their username.

## RadioID checks

DMRHub has five checks of DMR IDs and callsigns, most of them against the RadioID.net data. Each one is on by default. Turn each on or off under `dmr.radio-id-checks`:

| Key | When it is on |
|-----|---------------|
| `dmr.radio-id-checks.user-id` | DMRHub refuses a registration whose DMR ID does not have 7 digits. |
| `dmr.radio-id-checks.callsign` | DMRHub refuses a registration or a callsign change whose callsign RadioID.net does not list under the DMR ID of the account. The RadioID drift check covers the DMR ID of each account. |
| `dmr.radio-id-checks.radio-ids` | DMRHub refuses an added radio ID that RadioID.net does not list under the callsign of the user. The RadioID drift check covers the added radio IDs. |
| `dmr.radio-id-checks.repeater-ids` | DMRHub refuses a 6-digit repeater ID that RadioID.net does not list under the callsign of its owner, and fills in the details of the repeater from RadioID.net. |
| `dmr.radio-id-checks.hotspot-ids` | DMRHub refuses a 9-digit hotspot ID that does not start with the DMR ID of its owner. |

`dmr.disable-radio-id-validation` turns off all five checks, whatever they are set to. Older configs use it.

## Fix RadioID drift

A callsign can change on RadioID.net after a user registers. Each night at 01:00 (server time), DMRHub compares the DMR ID and the added radio IDs of each user with RadioID.net. Go to **Admin**, then **RadioID Drift** (`<siteurl>/admin/users/radio-id-drift`) to see the radio IDs that no longer match. This page shows only when the callsign check or the radio IDs check is on. The callsign check covers the DMR ID of each account, and the radio IDs check covers the added radio IDs.

- Click **Fix** to give the user the callsign that RadioID.net lists. If the user confirmed an email address, you can also send them an email about the change.
- **Not listed** means that RadioID.net no longer has the radio ID.
- A row has no **Fix** when RadioID.net does not have the radio ID, or lists an added radio ID under another callsign than the DMR ID of the user. Click **Edit user** to change the user.

When SMTP is on, DMRHub sends one email to a user with a confirmed address when the check first finds one of their radio IDs. If more than 5 radio IDs and more than 5% of the checked radio IDs drift in one check, DMRHub sends no emails and logs a warning, because a bad RadioID.net download is more likely than a real change.

## Logins

A login lasts 30 days. Change this with `http.session-length` on the Setup page, such as `12h` or `2160h`. The new length applies to each new login at once. A shorter length also ends the older logins that are past it. A longer length does not make the older logins last longer.

Turn on `login-backoff` to slow down password guessing. After 10 failed logins of one account in 15 minutes, DMRHub refuses each login of that account, even with the right password, until fewer than 10 failures are in the last 15 minutes. A login by the username and a login by the callsign count for the same account. A successful login clears the count. Other accounts are not affected. `login-backoff` is off by default.

## Suspend a user

On the **Users** page, turn on the **Suspend?** box of the user. A suspended user cannot log in, and DMRHub refuses the API calls of their open sessions. Turn the box off to let the user in again. You cannot suspend yourself.

## Reset the password of a user

1. On the **Users** page, click **Reset password** in the row of the user.
2. DMRHub shows a temporary password. Copy it, then click **Set password**.
3. Give the temporary password to the user.

DMRHub logs the user out everywhere. When the user logs in with the temporary password, DMRHub asks them to choose their own password before they can use the other pages. You cannot reset your own password here.

When SMTP is on, users can also reset their own password with **Forgot your password?** on the login page. DMRHub sends the link only to the email address of the account, and only when the user confirmed that address. The link works once, for 1 hour. Users set and confirm their address in the **Email** card on **Settings**.

<!-- screenshot: admin-reset-password.png | /admin/users after clicking Reset password: the "Reset the password of N0CALL" dialog with a temporary password -->

## Make a user an admin

Only the super admin can do this. On the **Users** page, turn on the **Admin?** box of the user. Turn it off to stop the user being an admin. Other admins get the message "Only the System Admin can do this."

## Delete a user

Only the super admin can do this. On the **Users** page, click **Delete** in the row of the user, then click **OK** in the browser message. Click **Cancel** to keep the user.

<!-- screenshot: admin-users.png | /admin/users with two users (N0CALL 3000001 admin, N1CALL 3000002), showing the Edit, Reset password and Delete buttons and the Suspend? and Admin? boxes -->
