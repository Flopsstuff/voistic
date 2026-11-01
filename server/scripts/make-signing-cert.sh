#!/bin/bash
# Creates the self-signed certificate `make build` signs with, in the login
# keychain. macOS ties permissions such as Accessibility to the signature, and an
# ad-hoc signature changes with every build, which would drop them each time.
set -euo pipefail

name="${1:-Voistic Local}"
keychain="$HOME/Library/Keychains/login.keychain-db"

if security find-certificate -c "$name" "$keychain" >/dev/null 2>&1; then
  echo "\"$name\" already exists."
  exit 0
fi

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
password=$(openssl rand -hex 16)

cat > "$dir/cert.conf" <<CONF
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = $name
[ext]
basicConstraints = critical, CA:false
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
CONF

/usr/bin/openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout "$dir/key.pem" -out "$dir/cert.pem" -config "$dir/cert.conf" 2>/dev/null
/usr/bin/openssl pkcs12 -export -inkey "$dir/key.pem" -in "$dir/cert.pem" \
  -name "$name" -out "$dir/cert.p12" -passout "pass:$password"
security import "$dir/cert.p12" -k "$keychain" -P "$password" -T /usr/bin/codesign -f pkcs12
echo "Created \"$name\"."
