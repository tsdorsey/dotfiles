#!/bin/zsh

NVM_DIR="/Users/`whoami`/.nvm"

if [[ -d "$NVM_DIR" ]]; then
  # This loads nvm
  export NVM_DIR
  [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"

  # place this after nvm initialization!
  autoload -U add-zsh-hook
  load-nvmrc() {
    local node_version="$(nvm version)"
    local nvmrc_path="$(nvm_find_nvmrc)"
    if [ -n "$nvmrc_path" ]; then
      local nvmrc_node_version=$(nvm version "$(cat "${nvmrc_path}")")

      if [ "$nvmrc_node_version" != "N/A" ] && [ "$nvmrc_node_version" != "$node_version" ]; then
        nvm use
      fi
    fi
  }

  add-zsh-hook chpwd load-nvmrc
  load-nvmrc
fi

