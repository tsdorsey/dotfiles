#!/bin/sh
#
# LaunchAgents
#
# Renders plist templates into ~/Library/LaunchAgents with the home
# directory substituted in.

# script/install sources this from the repo root.

agents_dir="$HOME/Library/LaunchAgents"
mkdir -p "$agents_dir"
mkdir -p "$HOME/logs"

for src in launchd/LaunchAgents/*.plist.template
do
  [ -f "$src" ] || continue

  name=$(basename "$src" .plist.template)
  dst="$agents_dir/${name}.plist"
  old_dst="$agents_dir/${name}"

  # Previous installer stripped .plist and symlinked; drop that leftover.
  if [ -L "$old_dst" ]; then
    rm "$old_dst"
  fi

  if [ -L "$dst" ]; then
    rm "$dst"
  fi

  sed "s|__HOME__|$HOME|g" "$src" > "$dst"
  echo "  Installed $dst"
done
