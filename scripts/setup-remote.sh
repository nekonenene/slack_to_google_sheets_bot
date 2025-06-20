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
echo ""
echo "📋 This script will:"
echo "   - Configure systemd service (requires sudo)"
echo "   - Setup firewall rules (requires sudo)"
echo "   - Copy environment files"
echo ""
echo "⚠️  Note: You may be prompted for the sudo password on the remote server"
echo "   during the setup process. This is normal and required for system configuration."
echo ""

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
cp -f deploy.env.example deploy.env
sed -i "" "s|REMOTE_HOST=server-hostname|REMOTE_HOST=$REMOTE_HOST|g" deploy.env
sed -i "" "s|REMOTE_USER=server-username|REMOTE_USER=$REMOTE_USER|g" deploy.env
echo "✅ deploy.env created and configured"
echo "Configuration:"
cat deploy.env

# Step 2: Setup remote server
echo "🔧 Setting up remote server..."
echo "Note: You may be prompted for sudo password on the remote server."

ssh $REMOTE_USER@$REMOTE_HOST "
    mkdir -p $REMOTE_PATH
"

# Copy systemd service file
scp systemd/slack-to-google-sheets-bot.service $REMOTE_USER@$REMOTE_HOST:/tmp/

echo "Configuring systemd service (sudo password may be required)..."
ssh -t $REMOTE_USER@$REMOTE_HOST "
    # Update paths in service file
    sed -i 's|/home/server-username/slack-to-google-sheets-bot|$REMOTE_PATH|g' /tmp/slack-to-google-sheets-bot.service
    sed -i 's|User=server-username|User=$REMOTE_USER|g' /tmp/slack-to-google-sheets-bot.service

    # Install service (requires sudo)
    sudo mkdir -p /etc/systemd/system/
    sudo mv /tmp/slack-to-google-sheets-bot.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable slack-to-google-sheets-bot

    # Configure firewall (ufw)
    echo '🔥 Configuring firewall...'

    # Check if ufw is installed
    if command -v ufw >/dev/null 2>&1; then
        sudo ufw allow 55999/tcp comment 'Slack to Google Sheets Bot'
        sudo ufw --force enable
        echo '✅ Firewall rule added: port 55999/tcp allowed'

        # Show current status
        echo 'Current firewall status:'
        sudo ufw status | grep -E '(Status|55999)' || echo 'Port 55999 rule added but not visible in brief status'
    else
        echo '⚠️  ufw not found. Please manually open port 55999 in your firewall'
        echo '   For other firewalls, refer to your system documentation'
    fi
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
echo "✅ Setup Summary:"
echo "   - Remote server configured with systemd service"
echo "   - Environment files (.env, credentials.json) copied"
echo "   - Firewall configured to allow port 55999"
echo "   - Auto-deployment ready"
echo ""
echo "Next steps:"
echo "1. Start auto-deployment:"
echo "   make watch-deploy"
echo ""
echo "2. Update your Slack app's Event Subscriptions URL to:"
echo "   http://$REMOTE_HOST:55999/slack/events"
echo ""
echo "3. Your bot will automatically restart when you change code or .env files!"
