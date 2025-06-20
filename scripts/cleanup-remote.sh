#!/bin/bash

# Remote server cleanup script
# Usage: ./scripts/cleanup-remote.sh <remote-host> <remote-user>

if [ $# -lt 2 ]; then
    echo "Usage: $0 <remote-host> <remote-user>"
    echo "Example: $0 server-hostname server-username"
    exit 1
fi

REMOTE_HOST=$1
REMOTE_USER=$2
REMOTE_PATH="/home/$REMOTE_USER/slack-to-google-sheets-bot"

echo "🧹 Cleaning up remote server: $REMOTE_USER@$REMOTE_HOST"

# Confirm before proceeding
read -p "This will remove the service and all files in $REMOTE_PATH. Continue? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cleanup cancelled."
    exit 1
fi

echo "Stopping and disabling service..."
ssh $REMOTE_USER@$REMOTE_HOST "
    sudo systemctl stop slack-to-google-sheets-bot 2>/dev/null || echo 'Service was not running'
    sudo systemctl disable slack-to-google-sheets-bot 2>/dev/null || echo 'Service was not enabled'
    sudo rm -f /etc/systemd/system/slack-to-google-sheets-bot.service
    sudo systemctl daemon-reload
"

echo "Removing application directory..."
ssh $REMOTE_USER@$REMOTE_HOST "
    if [ -d '$REMOTE_PATH' ]; then
        rm -rf '$REMOTE_PATH'
        echo 'Application directory removed'
    else
        echo 'Application directory not found'
    fi
"

echo "✅ Remote server cleanup completed!"
echo "The following have been removed:"
echo "  - systemd service: slack-to-google-sheets-bot"
echo "  - Application directory: $REMOTE_PATH"
