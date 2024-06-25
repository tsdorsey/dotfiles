#!/bin/sh
#

export NVM_DIR="$HOME/.nvm"
NVM_HOMEBREW="/usr/local/opt/nvm/nvm.sh"
[ -s "$NVM_HOMEBREW" ] && \. "$NVM_HOMEBREW"

[ -x "$(command -v npm)" ] && export NODE_PATH=$NODE_PATH:$(npm root -g)

# export NODE_PATH="/usr/local/lib/node_modules:$NODE_PATH"
