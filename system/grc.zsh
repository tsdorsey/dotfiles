#!/bin/sh
#

# GRC colorizes nifty unix tools all over the place
if [[ $(which grc) && $(which brew) ]]; then
  source "$(brew --prefix)/etc/grc.zsh"
fi
