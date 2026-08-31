#!/bin/sh
#
# AWS CLI
#
# This will install/upgrade the AWS CLI.

# script/install sources this from the repo root.
if [ ! -f aws.symlink/config ]; then
  echo "  Creating aws.symlink/config from example."
  cp aws.symlink/config.example aws.symlink/config
fi

# Check for awscli
if test ! "$(which aws)"; then
  echo "  Installing AWS cli for you."
  brew install awscli
else
  brew upgrade awscli
fi

