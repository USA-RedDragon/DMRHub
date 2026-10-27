# Create an account

You need an account to enroll a hotspot, pick talkgroups, read your text messages and see your positions. Anyone can see the **Home**, **Last Heard**, **Talkgroups** and **Nets** pages without an account.

## Before you start

- Get a DMR ID from [RadioID.net](https://radioid.net/). DMRHub checks that RadioID.net lists your DMR ID under your callsign, unless the admin turned this check off. DMRHub updates its copy of the RadioID.net database when it starts and then once a day at midnight, in the time zone of the server. A DMR ID that you got today can fail until the next update.
- Pick a password that you do not use anywhere else. On a network without HTTPS, such as some AREDN networks, other people on the network can see your password when you log in.

## Register

1. Go to `<siteurl>/register`, or click **Register** in the navigation bar.
2. Fill in the form:
   - **DMR ID**: your 7-digit DMR ID, for example 3100001.
   - **Username**: the name you log in with.
   - **Callsign**: your callsign, for example N0CALL.
   - **Password** and **Confirm Password**.
3. Click **Create User**.

<!-- screenshot: register.png | the registration form at /register, filled in with N0CALL and 3100001 -->

The message that comes up tells you what happens next:

- **Your account is ready. You can log in now.** The network approves new accounts at once.
- **User created, please wait for admin approval.** An admin must approve your account before you can log in.

### If registration fails

| Message | What to do |
| --- | --- |
| DMR ID is not valid | RadioID.net does not list this DMR ID. Check the number, or wait for the daily update. |
| Callsign does not match DMR ID | RadioID.net lists the DMR ID under another callsign. |
| Username is already taken | Pick another username. |
| DMR ID is already registered | The DMR ID belongs to another account on this network. Ask an admin. |
| Password has been reported in a data breach. Please use another one | The network checks passwords against [Have I Been Pwned](https://haveibeenpwned.com/), and your password is on its list. Pick another password. |

DMRHub stores only a salted Argon2id hash of your password, never the password itself.

## Log in

1. Go to `<siteurl>/login`, or click **Login**.
2. Enter your username or callsign, and your password.
3. Click **Log in**.

If an admin set a temporary password for you, DMRHub opens **Settings** and asks you to choose your own password before you can use the other pages. See [Manage your account](account.md).

## Forgot your password

The login page shows **Forgot your password?** only when the network can send email. DMRHub sends the reset link to the email address of your account. DMRHub has no page where you can add an email address to your account, so if no email comes, ask an admin to set a temporary password for you.

## Next

[Connect a hotspot](connect-hotspot.md)
