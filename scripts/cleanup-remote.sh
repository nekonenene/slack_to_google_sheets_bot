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
REMOTE_PATH="/home/$REMOTE_USER/slack-to-google-sheets-bot-dev"

echo "🧹 Cleaning up remote server: $REMOTE_USER@$REMOTE_HOST"
echo ""
echo "📋 This script will:"
echo "   - Stop and remove systemd service (requires sudo)"
echo "   - Remove firewall rules (requires sudo)"
echo "   - Delete application directory"
echo ""
echo "⚠️  Note: You may be prompted for the sudo password on the remote server"
echo "   during the cleanup process. This is normal and required for system cleanup."
echo ""

# Confirm before proceeding
read -p "This will remove the service and all files in $REMOTE_PATH. Continue? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cleanup cancelled."
    exit 1
fi

echo "Stopping and disabling service (sudo password may be required)..."
ssh -t $REMOTE_USER@$REMOTE_HOST "
    sudo systemctl stop slack-to-google-sheets-bot-dev 2>/dev/null || echo 'Service was not running'
    sudo systemctl disable slack-to-google-sheets-bot-dev 2>/dev/null || echo 'Service was not enabled'
    sudo rm -f /etc/systemd/system/slack-to-google-sheets-bot-dev.service
    sudo systemctl daemon-reload
"

echo "Removing firewall rule (sudo password may be required if not cached)..."
ssh -t $REMOTE_USER@$REMOTE_HOST "
    # Check if ufw is installed
    if command -v ufw >/dev/null 2>&1; then
        sudo ufw delete allow 55999/tcp 2>/dev/null && echo '✅ Firewall rule removed: port 55999/tcp' || echo '⚠️  Firewall rule for port 55999/tcp was not found'

        # Show current status
        echo 'Current firewall status:'
        sudo ufw status | grep -E '(Status|55999)' || echo 'Port 55999 rule successfully removed'
    else
        echo '⚠️  ufw not found. Please manually remove port 55999 from your firewall'
        echo '   For other firewalls:'
        echo '   - firewalld: sudo firewall-cmd --permanent --remove-port=55999/tcp && sudo firewall-cmd --reload'
        echo '   - iptables: sudo iptables -D INPUT -p tcp --dport 55999 -j ACCEPT'
    fi
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

echo ""
echo "✅ Remote server cleanup completed!"
echo ""
echo "🧹 Cleanup Summary:"
echo "   - systemd service: slack-to-google-sheets-bot-dev (removed)"
echo "   - Firewall rule for port 55999/tcp (removed if ufw was available)"
echo "   - Application directory: $REMOTE_PATH (removed)"
echo ""
echo "Note: If ufw was not available, please manually remove port 55999"
echo "from your firewall configuration."
