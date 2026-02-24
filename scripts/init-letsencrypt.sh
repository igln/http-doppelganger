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

if docker volume ls | grep -q "http-doppelganger_letsencrypt"; then
    echo "Checking for existing certificates..."
    if docker run --rm -v http-doppelganger_letsencrypt:/etc/letsencrypt alpine test -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem"; then
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

sleep 3

echo ""
echo "Step 2: Requesting certificate from Let's Encrypt..."
docker-compose -f docker-compose.init.yml run --rm certbot-init

echo ""
echo "Step 3: Stopping temporary nginx..."
docker-compose -f docker-compose.init.yml down

echo ""
echo "Step 4: Updating config.yaml with TLS settings..."
if grep -q "tls:" config.yaml; then
    echo "TLS section already exists in config.yaml"
else
    cat >> config.yaml << EOF

tls:
  enabled: true
  cert_file: "/etc/letsencrypt/live/${DOMAIN}/fullchain.pem"
  key_file: "/etc/letsencrypt/live/${DOMAIN}/privkey.pem"
  domain: "${DOMAIN}"
EOF
    echo "TLS configuration added to config.yaml"
fi

echo ""
echo "=== Certificate initialization complete! ==="
echo ""
echo "You can now start the GitLab proxy with:"
echo "  docker-compose up -d"
echo ""
echo "The certbot-renew service will automatically renew certificates before expiry."
