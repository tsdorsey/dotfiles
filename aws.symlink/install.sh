#!/bin/sh
#
# AWS CLI
#
# This will install/upgrade the AWS CLI.

# Check for awscli
if test ! $(which aws)
then
  echo "  Installing AWS cli for you."
  sudo -H pip3 install awscli --ignore-installed six
else
  sudo -H pip3 install --upgrade awscli --ignore-installed six
fi

# Check for awscli
if test ! $(which eb)
then
  echo "  Installing AWS cli for you."
  sudo -H pip3 install awsebcli --ignore-installed six
else
  sudo -H pip3 install --upgrade awsebcli --ignore-installed six
fi
