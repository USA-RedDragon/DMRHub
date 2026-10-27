# Users

This page tells how to approve new users, and how to edit, suspend and delete users. You must be an admin. Some actions need the super admin (see [Admins and the super admin](overview.md#admins-and-the-super-admin)).

## Approve new users

When a ham registers at `<siteurl>/register`, DMRHub checks that RadioID.net lists the DMR ID under the callsign. You can turn this check off with `dmr.disable-radio-id-validation`. If you set the HIBP API key (`hibp-api-key`), DMRHub also refuses passwords that are in the Have I Been Pwned list.

Then the new user waits for an admin, unless `auto-approve-users` is on. With `auto-approve-users` on, the new user can log in at once.

When SMTP is on, DMRHub sends an email about each new user to each admin that has a confirmed email address. The email has a link to the approval page.

To approve a user:

1. Go to **Admin**, then **User Approvals** (`<siteurl>/admin/users/approval`).
2. Find the user, and click **Approve** in the **Approve?** column.
3. The browser shows the message "Are you sure you want to approve this user?". Click **OK**. This message has no **Cancel** button: DMRHub approves the user when it closes.

The RadioID.net check shows that the DMR ID and the callsign go together. It does not show that the person who registered holds that callsign. Use your own judgment before you approve a user.

<!-- screenshot: admin-user-approvals.png | /admin/users/approval with one user waiting (N0CALL, 3000001), the Approve button visible -->

## Edit a user

1. Go to **Admin**, then **Users** (`<siteurl>/admin/users`).
2. Click **Edit** in the row of the user.
3. Change the **Callsign** or the **Username**, then click **Save Changes**. Click **Cancel** to keep the old values.

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
