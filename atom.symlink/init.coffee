# Add any auto-loaded Atom code on init here.
atom.commands.add 'atom-workspace',
  'tsdorsey:lighten-up': ->
    atom.config.set('core.themes', ["solarized-light-ui", "solarized-light-syntax"])

  'tsdorsey:go-dark': ->
    atom.config.set('core.themes', ["atom-solarized-dark-ui", "solarized-dark-syntax"])
