# atom-notify

This package simply provides methods for quickly creating atom notifications.

Right now, the only option is `dismissable`.

## Usage

``` coffeescript
notify = require 'atom-notify'

notifier = notify "Neat-o!" # All notifications from notifier will be title with 'Neat-o'
notifier.addSuccess "That was a blast!"
notifier.addError "That was a bummer!"
notifier.addInfo "There is air outside."
warning = notifier.addWarning "Your shoes are untied.", dismissable: true
# User ties their shoes...
warning.dismiss()
```
