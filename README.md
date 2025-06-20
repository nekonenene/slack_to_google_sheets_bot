# slack-to-google-sheets-bot

Slack app that records channel posts to Google Sheets.

## Requirements

- Go 1.24+
- Slack API token with `channels:history` and `chat:write` scopes
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

2. **Enable Google Sheets API**:
   - Go to **APIs & Services** → **Library**
   - Search for "Google Sheets API"
   - Click on it and press **Enable**

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
     - URL: `https://docs.google.com/spreadsheets/d/SPREADSHEET_ID/edit`
     - Copy the `SPREADSHEET_ID` part
   - **Important**: Share the spreadsheet with the service account:
     - Click **Share** in your spreadsheet
     - Add the service account email (found in `credentials.json` as `client_email`)
     - Give it **Editor** permissions

### 3. Environment Setup

1. **Copy environment template**:
   ```bash
   cp .env.example .env
   ```

2. **Fill in your credentials in `.env`**:
   ```bash
   SLACK_BOT_TOKEN=xoxb-1234567890-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx
   SLACK_SIGNING_SECRET=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
   GOOGLE_SHEETS_CREDENTIALS=./credentials.json
   SPREADSHEET_ID=1abCD2efGH3ijKL4mnOP5qrST6uvWX7yzAB8CdEfGh9I
   PORT=55999
   ```

   **Where to find these values**:
   - `SLACK_BOT_TOKEN`: From Slack app → OAuth & Permissions → Bot User OAuth Token
   - `SLACK_SIGNING_SECRET`: From Slack app → Basic Information → Signing Secret
   - `GOOGLE_SHEETS_CREDENTIALS`: Path to your downloaded `credentials.json` file
   - `SPREADSHEET_ID`: From your Google Sheets URL (the long ID between `/d/` and `/edit`)
   - `PORT`: The port your server will run on (55999 is recommended)

### 4. Development Setup

Choose your development approach:

#### Local Development
```bash
# Install dependencies and setup
make init

# Run locally
make run
```

#### Remote Development (Recommended)
For production-like environment with automatic deployment:

**Benefits:**
- No ngrok dependency - more secure
- Production-like environment testing
- Automatic builds and deployments on file changes
- systemd service management
- Real server performance testing

1. **Setup remote server**:
   ```bash
   ./scripts/setup-remote.sh server-hostname server-username
   ```

2. **Configure deployment**:
   ```bash
   cp deploy.env.example deploy.env
   # Edit deploy.env with your server details:
   # REMOTE_HOST=server-hostname
   # REMOTE_USER=server-username
   ```

3. **Copy environment and credentials**:
   ```bash
   # Copy your .env file
   scp .env server-username@server-hostname:/home/server-username/slack-to-google-sheets-bot/

   # Copy Google Sheets credentials
   scp path/to/service-account.json server-username@server-hostname:/home/server-username/slack-to-google-sheets-bot/credentials.json
   ```

4. **Start auto-deployment**:
   ```bash
   make watch-deploy
   ```

Now any changes to `.go` files will automatically build and deploy to your remote server!
The auto-deploy system also watches for changes to `.env` files and will sync them to the remote server and restart the service.

**Important:** After setting up your remote server, update your Slack app's Event Subscriptions URL to point to your server:
- Go to your Slack app settings → Event Subscriptions
- Update Request URL to: `http://your-server-ip:55999/slack/events`
- Make sure your server's port 55999 is open to the internet

**Security Note:** For production use, consider:
- Using HTTPS with SSL certificates (Let's Encrypt)
- Restricting firewall rules to Slack's IP ranges
- Using a reverse proxy (nginx/Apache) instead of direct port access

## Cleanup Remote Server

When you're done with development and want to clean up the remote server:

### Option A: Using cleanup script (Recommended)
```bash
./scripts/cleanup-remote.sh server-hostname server-username
```

### Option B: Manual cleanup
```bash
# Stop and disable the service
ssh server-username@server-hostname "sudo systemctl stop slack-to-google-sheets-bot"
ssh server-username@server-hostname "sudo systemctl disable slack-to-google-sheets-bot"

# Remove service file
ssh server-username@server-hostname "sudo rm /etc/systemd/system/slack-to-google-sheets-bot.service"

# Reload systemd
ssh server-username@server-hostname "sudo systemctl daemon-reload"

# Remove application directory (be careful with this command!)
ssh server-username@server-hostname "rm -rf /home/server-username/slack-to-google-sheets-bot"
```

**Warning:** These commands will permanently delete all files in the application directory, including your `.env` file and credentials. The cleanup script will ask for confirmation before proceeding.

## Manual Deployment Commands

```bash
# One-time deployment
make deploy

# Build for Linux (without deploying)
make build-linux

# Other useful commands
make fmt    # Format code
make vet    # Static analysis
make test   # Run tests
```

## Alternative: Local Development with ngrok

If you prefer local development with webhook testing:

```bash
# Install ngrok and run
ngrok http 55999

# Update your Slack app's Event Subscriptions URL to:
# https://your-ngrok-url.ngrok.io/slack/events
```

## Troubleshooting

### Google Sheets API Issues

**Error: "The caller does not have permission"**
- Make sure you shared the spreadsheet with the service account email
- The email is found in `credentials.json` as `client_email`
- Give the service account **Editor** permissions

**Error: "File not found" for credentials.json**
- Check the path in `GOOGLE_SHEETS_CREDENTIALS` environment variable
- Make sure the file exists and is readable
- Use relative path like `./credentials.json` or absolute path

### Slack API Issues

**Event URL verification failed**
- Make sure your server is accessible from the internet
- Check that the port (55999) is open in your firewall/security group
- Verify the URL format: `http://your-server-ip:55999/slack/events`

**Bot doesn't respond to events**
- Check that the bot is added to the channel
- Verify bot token starts with `xoxb-`
- Check application logs for error messages
