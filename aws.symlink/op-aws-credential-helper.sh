#!/usr/bin/env bash

VAULT_NAME="${1}"
ITEM_NAME="${2}"

if [[ -z "${VAULT_NAME}" || -z "${ITEM_NAME}" ]]; then
  echo "Usage: ${0} <vault_name> <item_name>"
  exit 1
fi

# Use op read for efficiency; falls back gracefully
ACCESS_KEY=$(op read "op://${VAULT_NAME}/${ITEM_NAME}/aws_access_key_id" 2>/dev/null)
SECRET_KEY=$(op read "op://${VAULT_NAME}/${ITEM_NAME}/aws_secret_access_key" 2>/dev/null)

# Output JSON expected by AWS
jq -n \
  --arg ak "$ACCESS_KEY" \
  --arg sk "$SECRET_KEY" \
  '{Version: 1, AccessKeyId: $ak, SecretAccessKey: $sk}'


