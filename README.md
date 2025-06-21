# slack-to-google-sheets-bot

Slack app that records channel posts to Google Sheets.

Automatically records posts from channels where the bot is invited.


## Requirements

- Go 1.24+
- Slack API token
- Google Sheets API credentials (Service Account)


## Setup

### 1. Slack API Setup

1. Go to [Slack API Dashboard](https://api.slack.com/apps)
2. Click "Create New App" → "From an app manifest"
3. Select your workspace
4. Copy the contents of `slack-app-manifest.yml` and paste it
5. Update the `request_url` in the manifest:
    - **For remote server**: `http://your-server-ip:55999/slack/events`
    - **For ngrok**: `https://your-ngrok-url.ngrok.io/slack/events`
6. Create the app
7. In **OAuth & Permissions**:
    - Install app to workspace
    - Copy the **Bot User OAuth Token** (starts with `xoxb-`)
8. In **Basic Information**:
    - Copy the **Signing Secret**

### 2. Google Sheets API Setup

1. **Setup Google Cloud Project**:
    - Go to [Google Cloud Console](https://console.cloud.google.com/)
    - Create a new project or select existing one
    - In the top navigation, make sure your project is selected

2. **Enable Required APIs**:
    - Go to **APIs & Services** → **Library**
    - Search for "Google Sheets API" and click **Enable**
    - Search for "Google Drive API" and click **Enable**
    - **Note**: Google Drive API is required for the "show me" command to grant spreadsheet access permissions

3. **Create Service Account**:
    - Go to **APIs & Services** → **Credentials**
    - Click **Create Credentials** → **Service Account**
    - Enter a name (e.g., "slack-sheets-bot")
    - Click **Create and Continue**
    - Skip role assignment (click **Continue**)
    - Click **Done**

4. **Download credentials.json**:
    - In the **Credentials** page, find your service account
    - Click on the service account email
    - Go to **Keys** tab
    - Click **Add Key** → **Create new key**
    - Select **JSON** format
    - Click **Create** - the `credentials.json` file will download automatically

5. **Setup Google Spreadsheet**:
    - Create a new Google Spreadsheet
    - Copy the spreadsheet ID from the URL:
      - URL: `https://docs.google.com/spreadsheets/d/GOOGLE_SPREADSHEET_ID/edit`
      - Copy the `GOOGLE_SPREADSHEET_ID` part
    - **Important**: Share the spreadsheet with the service account:
      - Click **Share** in your spreadsheet
      - Add the service account email (found in `credentials.json` as `client_email`)
      - Give it **Editor** permissions

### 3. Environment Setup

1. Run `make init`
2. **Fill in your credentials in `.env`**:
    ```bash
    SLACK_BOT_TOKEN=xoxb-your-bot-token
    SLACK_SIGNING_SECRET=your-signing-secret
    GOOGLE_SHEETS_CREDENTIALS='{ "type": "service_account", "project_id": "your-project-id", ... }'
    GOOGLE_SPREADSHEET_ID=our-spreadsheet-id
    PORT=55999
    ```

    **Where to find these values**:
    - `SLACK_BOT_TOKEN`: From Slack app → OAuth & Permissions → Bot User OAuth Token
    - `SLACK_SIGNING_SECRET`: From Slack app → Basic Information → Signing Secret
    - `GOOGLE_SHEETS_CREDENTIALS`: Body of `credentials.json` file
    - `GOOGLE_SPREADSHEET_ID`: From your Google Sheets URL (the long ID between `/d/` and `/edit`)
    - `PORT`: The port your server will run on (55999 is recommended)

### 4. Development Setup

Choose your development approach:

#### 4-1: Remote Development (Recommended)
For production-like environment with automatic deployment:

**Benefits:**
- No ngrok dependency - more secure
- Production-like environment testing
- Automatic builds and deployments on file changes
- systemd service management
- Real server performance testing

**One-command setup**:
```bash
# Make sure you have .env and credentials.json in your project root
./scripts/setup-remote.sh server-hostname server-username
```

This script will automatically:
1. Create and configure `deploy.env` with your server details
2. Setup the remote server with systemd service
3. Copy your `.env` and `credentials.json` files to the server

**Start development**:

```bash
make watch-deploy
```

Now any changes to `.go` or `.env` files will automatically build and deploy to your remote server!

**Important:** After setting up your remote server, update your Slack app's Event Subscriptions URL to point to your server:
- Go to your Slack app settings → Event Subscriptions
- Update Request URL to: `http://your-server-ip:55999/slack/events`
- Make sure your server's port 55999 is open to the internet

**Security Note:** For production use, consider:
- Using HTTPS with SSL certificates (Let's Encrypt)
- Restricting firewall rules to Slack's IP ranges
- Using a reverse proxy (nginx/Apache) instead of direct port access

##### Cleanup Remote Server

When you're done with development and want to clean up the remote server, you can run the cleanup script.

**Warning:** These commands will permanently delete all files in the application directory, including your `.env` file and credentials. The cleanup script will ask for confirmation before proceeding.

```bash
./scripts/cleanup-remote.sh server-hostname server-username
```

#### 4-2: Local Development

```bash
# Install dependencies and setup
make init

# Run locally
make run
```

With webhook testing, you can use ngrok, but I don't recommend it for security reasons.

```bash
# Install ngrok and run
ngrok http 55999

# Update your Slack app's Event Subscriptions URL to:
# https://your-ngrok-url.ngrok.io/slack/events
```


## Troubleshooting

### Google Sheets API Issues

#### Error: "The caller does not have permission"

- Make sure you shared the spreadsheet with the service account email
- The email is found in `credentials.json` as `client_email`
- Give the service account **Editor** permissions

### Slack API Issues

#### Event URL verification failed

- Make sure your server is accessible from the internet
- Check that the port (55999) is open in your firewall
- Verify the URL format: `http://your-server-ip:55999/slack/events`

#### Bot doesn't respond to events

- Check that the bot is added to the channel
- Verify bot token starts with `xoxb-`
- Check application logs for error messages
