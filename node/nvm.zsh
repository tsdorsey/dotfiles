#!/bin/zsh

NVM_DIR="/Users/`whoami`/.nvm"

if [[ -d "$NVM_DIR" ]]; then
  # This loads nvm
  export NVM_DIR
  [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"

  # place this after nvm initialization!
  autoload -U add-zsh-hook
  load-nvmrc() {
    if [[ -f .nvmrc && -r .nvmrc ]]; then
      current=$(nvm current)
      specified=$(sed -n '1p' .nvmrc)
      if [ $current != $specified ]; then
        nvm use
      else
        echo "Found '$(pwd)/.nvmrc' with version <$current>"
        echo "Now using node $current"
      fi
    fi
  }

  add-zsh-hook chpwd load-nvmrc
  load-nvmrc
fi
