#!/bin/sh
#

brew install nvm
mkdir -p ~/.nvm

source ./node_path.zsh

nvm install --lts
nvm use node
