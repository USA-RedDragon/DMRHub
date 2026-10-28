# Manage your account

Go to **Settings** (`<siteurl>/settings`) to change your password, set your email address, turn SMS Storage on or off, add your other radio IDs, and remove apps.

![The Settings page](../Screenshots/settings.png)

## Change your password

1. In the **Password** card, type your **Current password**.
2. Type the **New password** and type it again in **Confirm new password**.
3. Click **Change password**.

DMRHub logs out your other sessions.

If an admin set a temporary password for you, DMRHub shows only the Password card until you choose your own password. Until then, the other pages send you back to Settings.

## Email

DMRHub sends password reset links only to a confirmed email address.

1. In the **Email** card, type your address in **Email address** and click **Save**. To remove the address, clear the field and click **Save**.
2. DMRHub sends a link to the address. Open it to confirm the address. The link works once, for 24 hours. The card shows **Not confirmed** until you open it, and **Confirmed** after.

When you change the address, the new one is not confirmed until you open its link. A link sent to an older address no longer works.

![The Email card with n0call@example.com, not confirmed](../Screenshots/settings-email.png)

If the network cannot send email, the card says so instead. Then only an admin can reset your password.

## SMS Storage and radio IDs

See [Send and read text messages](text-messages.md#turn-on-sms-storage).

## Apps

Some web apps, such as the [DMRHub Browser Radio](browser-radio.md), can use your DMRHub account. The first time, DMRHub asks you on its own page, **Allow this app to use your DMRHub account?** The page shows the address of the app and a list of what it can do. Click **Allow** or **Don't allow**.

An app can never change your password or account, and never gets admin access.

![The consent page of DMRHub for the app http://localhost:8080, with the Allow and Don't allow buttons](../Screenshots/oauth-consent.png)

The **Apps** card on Settings lists the apps that you allowed, what each one can use, when you allowed it and when it last used your account. Click **Remove** to take away the access of an app. DMRHub signs it out at once.

An app keeps its access while you use it. If you do not use an app for 30 days, its login ends and you must log in again in the app.

## Theme

Use **Theme** at the bottom of each page to pick **Light**, **Dark** or **System**. Your browser remembers the choice.

## Log out

Click **Logout** in the navigation bar.

## Delete your account

You cannot delete your account yourself. Ask an admin.
