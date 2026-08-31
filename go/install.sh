#!/bin/sh
#
# Go
#
# This installs Go Version Manager

if test ! "$(which gvm)"; then
  echo "  Installing go for you."
  bash < <(curl -s -S -L https://raw.githubusercontent.com/moovweb/gvm/master/binscripts/gvm-installer)
fi


