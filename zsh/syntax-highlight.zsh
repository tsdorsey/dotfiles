fileToSource="$(brew --prefix)/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh"

if test -f "$fileToSource"; then
  source "$fileToSource"
else
  echo "zsh-syntax-highlighting not found at $fileToSource"
fi
