#!/bin/sh
#

if [ ! "$(which nvm)" ]; then
  curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash
fi
