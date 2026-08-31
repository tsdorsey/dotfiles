# Launch Control PList files

## User agents (`LaunchAgents/*.plist.template`)

`install.sh` (picked up by `script/install`) renders each template into
`~/Library/LaunchAgents`. `__HOME__` is replaced with the current user's
home directory.

## System daemons (`LaunchDaemons/`)

Not installed yet. These need root and belong in `/Library/LaunchDaemons`.
