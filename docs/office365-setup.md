# Office 365 Setup

pmx connects to Office 365 Exchange Online using OAuth2 over IMAP/SMTP with
the XOAUTH2 SASL mechanism. Unlike Gmail, Office 365 supports a publicly
registered application client — the same one Thunderbird uses — so **no Azure
app registration is required** for most personal and business accounts.

---

## Which path applies to you?

| Scenario | Path |
|---|---|
| Personal Microsoft account (`@outlook.com`, `@hotmail.com`, `@live.com`) | [Use the public client](#path-a--public-client-no-app-registration) |
| Work or school account (your organisation uses Microsoft 365) | [Use the public client](#path-a--public-client-no-app-registration) — unless your tenant admin has disabled third-party OAuth apps |
| Your tenant admin requires a registered app | [Register your own Azure app](#path-b--register-your-own-azure-app) |

---

## Path A — Public client (no app registration)

pmx uses [Thunderbird's public Azure client ID](https://hg.mozilla.org/comm-central/file/tip/mailnews/base/src/OAuth2Providers.sys.mjs)
(`9e5f94bc-e8a4-4e73-b8be-63364c29d753`). Microsoft pre-consents this
application for IMAP, SMTP, and POP3 access on personal accounts, and most
organisations do not restrict it. No admin consent is needed.

### Run the setup wizard

```
pmx account add --provider o365
```

pmx will prompt for the following:

| Prompt | What to enter |
|---|---|
| `Email address:` | Your Office 365 address, e.g. `you@example.com` |
| `Full name (for From header):` | Your display name |
| `Azure AD client ID:` | `9e5f94bc-e8a4-4e73-b8be-63364c29d753` |
| `Azure AD tenant [common]:` | Press Enter to accept `common` (works for personal accounts and most organisations). If sign-in fails, try your tenant ID (see [Finding your tenant ID](#finding-your-tenant-id)). |
| `Account name [you]:` | A short local name for this account |

After you enter these details pmx opens your browser. Sign in with your
Microsoft account. If your organisation requires MFA, complete it as normal.

Once approved the browser shows:

> Sign-in complete. You can close this tab and return to pmx.

---

## Path B — Register your own Azure app

Use this path if your organisation's conditional access policy blocks the
Thunderbird client ID, or if you want full control over the registered app.

### 1. Register the app in Azure

1. Go to <https://portal.azure.com> and sign in with an account that has
   permission to register apps in your tenant (your own account is sufficient
   for single-tenant personal use).
2. Open **Azure Active Directory → App registrations → New registration**.
3. Fill in:
   - **Name** — e.g. `pmx`
   - **Supported account types** — choose:
     - *Accounts in this organizational directory only* for a single corporate
       tenant
     - *Personal Microsoft accounts only* for `@outlook.com` / `@hotmail.com`
     - *Accounts in any organizational directory and personal Microsoft accounts*
       for both
   - **Redirect URI** — select **Public client/native (mobile & desktop)** from
     the dropdown and enter `http://localhost` (no port — Azure accepts any
     localhost port for public clients).
4. Click **Register**.
5. Copy the **Application (client) ID** from the overview page.

### 2. Add API permissions

1. In your app registration, go to **API permissions → Add a permission**.
2. Choose **APIs my organization uses** and search for **Office 365 Exchange
   Online** (application ID `00000002-0000-0ff1-ce00-000000000000`).
3. Under **Delegated permissions** add:
   - `IMAP.AccessAsUser.All`
   - `SMTP.Send`
   - `offline_access`
4. Click **Add permissions**.
5. If you are an admin on the tenant and want to skip the per-user consent
   prompt, click **Grant admin consent**. Otherwise each user will see a
   consent screen on first sign-in.

### 3. Run the setup wizard

```
pmx account add --provider o365
```

Enter the **Application (client) ID** you copied in step 1 when prompted for
the Azure AD client ID. For the tenant, enter your tenant ID (see
[Finding your tenant ID](#finding-your-tenant-id)) rather than `common` — this
ensures the sign-in page is scoped to your organisation.

---

## What pmx writes to config.toml

```toml
[[accounts]]
name                    = "work"
full_name               = "Your Name"
email                   = "you@example.com"
auth                    = "oauth2"
oauth_client_id         = "9e5f94bc-e8a4-4e73-b8be-63364c29d753"
oauth_tenant            = "e4d05607-4f93-4a30-93a5-0b875dd5d24c"
oauth_token_keyring_ref = "work"
imap_host               = "outlook.office365.com"
imap_port               = 993
imap_tls                = true
smtp_host               = "smtp.office365.com"
smtp_port               = 587
smtp_starttls           = true
```

No client secret is written — the Thunderbird public client (and any public
client you register yourself using the native redirect URI) authenticates via
PKCE without a secret. The OAuth access and refresh tokens are stored in the
system keyring under `service=pmx-oauth, account=<name>`.

---

## Manual setup (skip the wizard)

If you already have a valid OAuth token from another tool (e.g. mutt_oauth2.py,
Thunderbird, or another mail client) you can inject it directly and skip the
browser flow entirely.

### 1. Write the account block to config.toml

Append the following to `~/.config/pmx/config.toml`, filling in your own
values:

```toml
[[accounts]]
name                    = "work"
full_name               = "Your Name"
email                   = "you@example.com"
auth                    = "oauth2"
oauth_client_id         = "<client-id>"
oauth_tenant            = "<tenant-id-or-common>"
oauth_token_keyring_ref = "work"
imap_host               = "outlook.office365.com"
imap_port               = 993
imap_tls                = true
smtp_host               = "smtp.office365.com"
smtp_port               = 587
smtp_starttls           = true
```

Validate the result: `pmx config validate`

### 2. Convert and inject the token

If your existing token is in mutt_oauth2.py's GPG-encrypted format:

```sh
gpg -d ~/.cache/mutt/<account>-oauth2.gpg | python3 -c "
import sys, json
d = json.load(sys.stdin)
expiry = d.get('access_token_expiration', '')
if expiry and not expiry.endswith('Z') and '+' not in expiry:
    expiry += 'Z'
import json; print(json.dumps({
    'access_token':  d['access_token'],
    'refresh_token': d['refresh_token'],
    'token_type':    d.get('token_type', 'Bearer'),
    'expiry':        expiry,
}))
" | secret-tool store --label='pmx oauth work' service pmx-oauth account work
```

Replace `work` with the `oauth_token_keyring_ref` value you used in the config.

The access token will be refreshed automatically on first use if it has already
expired — only the refresh token needs to be valid.

---

## Finding your tenant ID

Your tenant ID is the GUID in the `iss` claim of any Microsoft JWT token, or
you can find it at:

- **Azure Portal** → Azure Active Directory → Overview → **Tenant ID**
- **PowerShell**: `(Get-AzTenant).Id`
- **Browser**: visit `https://login.microsoftonline.com/<your-domain>/.well-known/openid-configuration`
  and look for `"issuer": "https://sts.windows.net/<tenant-id>/"`.

Use `common` if you are unsure — it works for personal accounts and any
organisation that has not disabled cross-tenant token issuance.

---

## Token refresh

Office 365 refresh tokens are long-lived (typically 90 days of inactivity
before expiry, indefinite with regular use). pmx refreshes the access token
automatically. If the refresh token expires — for example after a long period
of disuse, a password change, or a revocation by an admin — re-run
`pmx account add --provider o365` with the same account name to replace the
stored token.

---

## Troubleshooting

**"AADSTS50020: User account from identity provider does not exist in tenant"**
You signed in with a personal Microsoft account but the app is registered as
single-tenant (organisation only). Either change the app's supported account
types to include personal accounts, or use the `common` tenant and the
Thunderbird client ID.

**"AADSTS65001: The user or administrator has not consented to use the
application"**
The client ID you are using is unknown to the tenant. Either use the Thunderbird
public client ID, or register your own app and grant admin consent (Path B).

**"AADSTS7000218: The request body must contain the following parameter:
client_assertion or client_secret"**
Your app registration requires a client secret. This happens when the redirect
URI is set to the web type instead of the native/public client type. Edit the
app registration: under **Authentication**, ensure the redirect URI platform is
set to **Mobile and desktop applications**, not **Web**.

**Browser does not open**
pmx prints the full URL to the terminal. Copy and paste it into a browser
manually.

**Sign-in succeeds but pmx shows authentication errors immediately**
This can happen when the tenant requires a specific named tenant in the token
URL rather than `common`. Re-run the setup and enter your tenant ID (see
[Finding your tenant ID](#finding-your-tenant-id)) instead of pressing Enter at
the tenant prompt.
