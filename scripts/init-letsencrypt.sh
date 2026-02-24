#!/bin/bash

set -e

if [ -z "$DOMAIN" ] || [ -z "$EMAIL" ]; then
    echo "Error: DOMAIN and EMAIL environment variables are required"
    echo "Usage: DOMAIN=gitlab.example.com EMAIL=admin@example.com ./init-letsencrypt.sh"
    exit 1
fi

echo "=== GitLab Proxy Let's Encrypt Certificate Initialization ==="
echo "Domain: $DOMAIN"
echo "Email: $EMAIL"
echo ""

PROJECT_NAME=$(basename "$(pwd)" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g')

if docker volume ls | grep -q "${PROJECT_NAME}_letsencrypt"; then
    echo "Checking for existing certificates..."
    if docker run --rm -v ${PROJECT_NAME}_letsencrypt:/etc/letsencrypt alpine test -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem"; then
        echo "Certificate already exists for $DOMAIN"
        read -p "Do you want to force renewal? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Keeping existing certificate. Run 'docker-compose up -d' to start the proxy."
            exit 0
        fi
    fi
fi

echo ""
echo "Step 1: Starting temporary nginx for ACME challenge..."
docker-compose -f docker-compose.init.yml up -d nginx-init

sleep 5

echo ""
echo "Step 2: Requesting certificate from Let's Encrypt..."
docker-compose -f docker-compose.init.yml run --rm certbot-init

echo ""
echo "Step 3: Stopping temporary nginx..."
docker-compose -f docker-compose.init.yml down

echo ""
echo "Step 4: Updating config.yaml with TLS settings..."

if grep -q "enabled: false" config.yaml; then
    sed -i.bak "s|enabled: false|enabled: true|g" config.yaml
    sed -i.bak "s|YOUR_DOMAIN|${DOMAIN}|g" config.yaml
    rm -f config.yaml.bak
    echo "TLS configuration updated in config.yaml"
elif grep -q "YOUR_DOMAIN" config.yaml; then
    sed -i.bak "s|YOUR_DOMAIN|${DOMAIN}|g" config.yaml
    rm -f config.yaml.bak
    echo "Domain updated in config.yaml"
else
    echo "TLS section already configured in config.yaml"
fi

echo ""
echo "=== Certificate initialization complete! ==="
echo ""
echo "IMPORTANT: Make sure to update config.yaml with your GitLab settings:"
echo "  - gitlab.host: Your internal GitLab hostname"
echo "  - gitlab.external_url: GitLab's configured external_url (for URL rewriting)"
echo ""
echo "You can now start the GitLab proxy with:"
echo "  docker-compose up -d"
echo ""
echo "The certbot-renew service will automatically renew certificates before expiry."
