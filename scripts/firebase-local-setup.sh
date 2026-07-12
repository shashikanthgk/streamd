#!/usr/bin/env bash
# Finish local Firebase setup after CLI init/deploy.
set -euo pipefail

CONFIG_DIR="${HOME}/.streamd"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
KEY_FILE="${CONFIG_DIR}/service-account.json"
PROJECT_ID="streamd-p2p-signaling"

mkdir -p "${CONFIG_DIR}"

if [[ ! -f "${CONFIG_FILE}" ]]; then
  cp configs/config.example.yaml "${CONFIG_FILE}"
  echo "Created ${CONFIG_FILE}"
else
  echo "Config already exists: ${CONFIG_FILE}"
fi

if [[ ! -f "${KEY_FILE}" ]]; then
  echo ""
  echo "Download a service account key and save it to:"
  echo "  ${KEY_FILE}"
  echo ""
  echo "Open:"
  echo "  https://console.firebase.google.com/project/${PROJECT_ID}/settings/serviceaccounts/adminsdk"
  echo ""
  echo "Click 'Generate new private key' and move the JSON file to the path above."
else
  echo "Service account key found: ${KEY_FILE}"
fi

echo ""
echo "Firebase project: ${PROJECT_ID}"
echo "Console: https://console.firebase.google.com/project/${PROJECT_ID}/overview"
echo "Firestore: https://console.firebase.google.com/project/${PROJECT_ID}/firestore"
