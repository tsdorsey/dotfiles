#!/bin/sh
#
# AWS CLI
#
# This will install/upgrade the AWS CLI.

# Check for awscli
if test ! $(which aws)
then
  echo "  Installing AWS cli for you."
  sudo -H pip install awscli --ignore-installed six
else
  sudo -H pip install --upgrade awscli --ignore-installed six
fi

# Check for awscli
if test ! $(which eb)
then
  echo "  Installing AWS cli for you."
  sudo -H pip install awsebcli --ignore-installed six
else
  sudo -H pip install --upgrade awsebcli --ignore-installed six
fi

exit 0
