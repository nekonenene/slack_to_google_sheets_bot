#!/bin/bash

# Remote server setup script
# Usage: ./scripts/setup-remote.sh <remote-host> <remote-user>

if [ $# -lt 2 ]; then
    echo "Usage: $0 <remote-host> <remote-user>"
    echo "Example: $0 server-hostname server-username"
    exit 1
fi

REMOTE_HOST=$1
REMOTE_USER=$2
REMOTE_PATH="/home/$REMOTE_USER/slack-to-google-sheets-bot"

echo "Setting up remote server: $REMOTE_USER@$REMOTE_HOST"

# Create directory structure on remote server
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
"

echo "✅ Remote server setup completed!"
echo "Next steps:"
echo "1. Copy your .env file to $REMOTE_PATH/"
echo "2. Copy your Google Sheets credentials to $REMOTE_PATH/"
echo "3. Run: make deploy"
