# Gmail Setup

pmx connects to Gmail using OAuth2 over IMAP/SMTP. Google requires each app to
have its own registered credentials, so there is a one-time setup in Google
Cloud Console before running `pmx account add`.

## Prerequisites

- A Google account (personal `@gmail.com` or Google Workspace)
- A browser available on the machine running pmx (or the ability to open a URL
  on another device)

---

## Step 1 — Create a Google Cloud project

1. Go to <https://console.cloud.google.com/>.
2. Click the project picker at the top of the page and choose **New Project**.
3. Give the project any name (e.g. `pmx-mail`). The organisation field can be
   left blank for personal accounts.
4. Click **Create** and wait for the project to be provisioned.

---

## Step 2 — Enable the Gmail API

1. With your new project selected, open the navigation menu and go to
   **APIs & Services → Library**.
2. Search for **Gmail API** and click on it.
3. Click **Enable**.

---

## Step 3 — Configure the OAuth consent screen

1. Go to **APIs & Services → OAuth consent screen**.
2. Select **External** as the user type and click **Create**.
3. Fill in the required fields:
   - **App name** — anything recognisable, e.g. `pmx`
   - **User support email** — your own address
   - **Developer contact information** — your own address
4. Click **Save and Continue** through the Scopes and Optional Info pages
   (no changes needed on those steps).
5. On the **Test users** page, click **Add Users** and add your own Gmail
   address. This is required — only listed test users can sign in while the app
   is in testing mode.
6. Click **Save and Continue**, then **Back to Dashboard**.

> **Note on verification:** Google requires apps that use sensitive scopes
> (like Gmail access) to go through a verification process before they can be
> used by more than 100 users. For personal use you can leave the app in
> **Testing** mode permanently — test users you add explicitly are never
> subject to that limit.

---

## Step 4 — Create OAuth credentials

1. Go to **APIs & Services → Credentials**.
2. Click **Create Credentials → OAuth client ID**.
3. Set the application type to **Desktop app**.
4. Give it a name (e.g. `pmx desktop`) and click **Create**.
5. A dialog will show your **Client ID** and **Client Secret**. Copy both —
   you will need them in the next step.

---

## Step 5 — Add the account to pmx

Run:

```
pmx account add --provider gmail
```

pmx will prompt for the following:

| Prompt | What to enter |
|---|---|
| `Email address:` | Your Gmail address, e.g. `you@gmail.com` |
| `Full name (for From header):` | Your display name |
| `Google OAuth client ID:` | The client ID from Step 4 |
| `Google OAuth client secret:` | The client secret from Step 4 |
| `Account name [you]:` | A short local name for this account (default is the part before `@`) |

After you enter these details pmx will print a URL and attempt to open your
browser. Sign in with the Gmail account you added as a test user in Step 3. You
will see a warning that the app is **unverified** — click **Continue** (this is
expected for a self-registered app in testing mode).

Once you approve the permissions, the browser will show:

> Sign-in complete. You can close this tab and return to pmx.

pmx stores the refresh token in your system keyring and appends the account
block to `~/.config/pmx/config.toml`.

---

## What pmx writes to config.toml

```toml
[[accounts]]
name                    = "you"
full_name               = "Your Name"
email                   = "you@gmail.com"
auth                    = "oauth2"
oauth_client_id         = "123456789-abc...apps.googleusercontent.com"
oauth_client_secret     = "GOCSPX-..."
oauth_token_keyring_ref = "you"
imap_host               = "imap.gmail.com"
imap_port               = 993
imap_tls                = true
smtp_host               = "smtp.gmail.com"
smtp_port               = 587
smtp_starttls           = true
```

The client secret is stored in config (not the keyring) because it is a
Desktop-app credential — Google explicitly states it is not truly secret for
installed applications. The OAuth access and refresh tokens are stored in the
system keyring under `service=pmx-oauth, account=<name>`.

---

## Token refresh

pmx automatically refreshes the access token using the stored refresh token.
Google refresh tokens for Desktop apps do not expire as long as the app is used
at least once every 6 months. If the token does expire or is revoked, re-run
`pmx account add --provider gmail` with the same account name to replace it.

---

## Troubleshooting

**"This app is blocked" during sign-in**
The Gmail address you are signing in with is not on the test users list. Go to
**OAuth consent screen → Test users** and add it.

**"Error 400: redirect_uri_mismatch"**
This should not occur — pmx uses a dynamic `http://127.0.0.1:<port>/callback`
redirect and Desktop-app credentials accept any localhost port. If you see
this, confirm the credential type is **Desktop app**, not **Web application**.

**Browser does not open**
pmx prints the full URL to the terminal. Copy and paste it into a browser
manually. The loopback server listens for up to the context deadline; if it
times out before you complete sign-in, re-run the command.

**"invalid_grant" on first use after adding the account**
The access token in the keyring is expired. pmx will automatically refresh it
using the refresh token — if that also fails, the refresh token may have been
revoked. Re-run `pmx account add --provider gmail` to re-authenticate.
