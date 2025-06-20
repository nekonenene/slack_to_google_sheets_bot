# slack-to-google-sheets-bot

Slack app that records channel posts to Google Sheets.

## Requirements

- Go 1.24+
- Slack API token with `channels:history` and `chat:write` scopes
- Google Sheets API credentials (Service Account)

## Setup

### 1. Slack API Setup

#### Option A: Using App Manifest (Recommended)

1. Go to [Slack API Dashboard](https://api.slack.com/apps)
2. Click "Create New App" → "From an app manifest"
3. Select your workspace
4. Copy the contents of `slack-app-manifest.yml` and paste it
5. Update the `request_url` in the manifest:
   - **For remote server**: `http://your-server-ip:55999/slack/events`
   - **For ngrok**: `https://your-ngrok-url.ngrok.io/slack/events`
6. Create the app
7. Install the app to your workspace
8. Copy the **Bot User OAuth Token** and **Signing Secret**

#### Option B: Manual Setup

1. Go to [Slack API Dashboard](https://api.slack.com/apps)
2. Click "Create New App" → "From scratch"
3. Enter app name and select workspace
4. In **OAuth & Permissions**:
   - Add Bot Token Scopes: `channels:history`, `channels:read`, `chat:write`, `groups:history`, `groups:read`, `im:history`, `im:read`, `mpim:history`, `mpim:read`
   - Install app to workspace
   - Copy the **Bot User OAuth Token** (starts with `xoxb-`)
5. In **Basic Information**:
   - Copy the **Signing Secret**
6. In **Event Subscriptions**:
   - Enable Events
   - Set Request URL to your server endpoint `/slack/events`
   - Subscribe to bot events: `member_joined_channel`, `message.channels`, `message.groups`, `message.im`, `message.mpim`

### 2. Google Sheets API Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing one
3. Enable the **Google Sheets API**
4. Create credentials:
   - Go to **Credentials** → **Create Credentials** → **Service Account**
   - Download the JSON key file
   - Share your target spreadsheet with the service account email
5. Create a new Google Spreadsheet and copy its ID from the URL

### 3. Environment Setup

1. Copy environment template:
   ```bash
   cp .env.example .env
   ```

2. Fill in your credentials in `.env`:
   ```bash
   SLACK_BOT_TOKEN=xoxb-your-bot-token
   SLACK_SIGNING_SECRET=your-signing-secret
   GOOGLE_SHEETS_CREDENTIALS=path/to/service-account.json
   SPREADSHEET_ID=your-spreadsheet-id
   PORT=55999
   ```

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
