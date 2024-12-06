fileToSource="$(brew --prefix)/share/zsh-autosuggestions/zsh-autosuggestions.zsh"

if test -f "$fileToSource"; then
  source "$fileToSource"
else
  echo "zsh-autosuggestions not found at $fileToSource"
fi
