#!/bin/sh
#
# herdr
#
# This installs herdr

if test ! "$(which herdr)"; then
  echo "  Installing herdr for you."
  brew install herdr
fi
