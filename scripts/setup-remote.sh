#!/bin/bash

# Remote server setup script with full automation
# Usage: ./scripts/setup-remote.sh <remote-host> <remote-user>

if [ $# -lt 2 ]; then
    echo "Usage: $0 <remote-host> <remote-user>"
    echo "Example: $0 server-hostname server-username"
    exit 1
fi

REMOTE_HOST=$1
REMOTE_USER=$2
REMOTE_PATH="/home/$REMOTE_USER/slack-to-google-sheets-bot"

echo "🚀 Setting up remote development environment: $REMOTE_USER@$REMOTE_HOST"

# Check if required files exist
if [ ! -f ".env" ]; then
    echo "❌ .env file not found. Please create it first:"
    echo "   cp .env.example .env"
    echo "   # Edit .env with your credentials"
    exit 1
fi

if [ ! -f "credentials.json" ]; then
    echo "❌ credentials.json not found. Please download it from Google Cloud Console first."
    exit 1
fi

echo "✅ Required files found"

# Step 1: Create deployment configuration
echo "📝 Creating deployment configuration..."
if [ ! -f "deploy.env" ]; then
    cp deploy.env.example deploy.env
    sed -i "s/REMOTE_HOST=server-hostname/REMOTE_HOST=$REMOTE_HOST/" deploy.env
    sed -i "s/REMOTE_USER=server-username/REMOTE_USER=$REMOTE_USER/" deploy.env
    echo "✅ deploy.env created and configured"
else
    echo "⚠️  deploy.env already exists, skipping creation"
fi

# Step 2: Setup remote server
echo "🔧 Setting up remote server..."
ssh $REMOTE_USER@$REMOTE_HOST "
    mkdir -p $REMOTE_PATH
    sudo mkdir -p /etc/systemd/system/
"

# Copy systemd service file
scp systemd/slack-to-google-sheets-bot.service $REMOTE_USER@$REMOTE_HOST:/tmp/
ssh $REMOTE_USER@$REMOTE_HOST "
    # Update paths in service file
    sed -i 's|/home/server-username/slack-to-google-sheets-bot|$REMOTE_PATH|g' /tmp/slack-to-google-sheets-bot.service
    sed -i 's|User=server-username|User=$REMOTE_USER|g' /tmp/slack-to-google-sheets-bot.service

    # Install service
    sudo mv /tmp/slack-to-google-sheets-bot.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable slack-to-google-sheets-bot

    # Configure firewall (ufw)
    echo '🔥 Configuring firewall...'
    sudo ufw allow 55999/tcp comment 'Slack to Google Sheets Bot'
    sudo ufw --force enable
"

echo "✅ Remote server configured"

# Step 3: Copy credentials and environment files
echo "📁 Copying credentials and environment files..."
scp .env $REMOTE_USER@$REMOTE_HOST:$REMOTE_PATH/
scp credentials.json $REMOTE_USER@$REMOTE_HOST:$REMOTE_PATH/

echo "✅ Files copied to remote server"

echo ""
echo "🎉 Remote development environment setup completed!"
echo ""
echo "Next steps:"
echo "1. Start auto-deployment:"
echo "   make watch-deploy"
echo ""
echo "2. Update your Slack app's Event Subscriptions URL to:"
echo "   http://$REMOTE_HOST:55999/slack/events"
echo ""
echo "3. Make sure port 55999 is open on your server firewall"
