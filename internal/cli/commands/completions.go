package commands


// RunCompletions emits a static completion script for the requested shell.
// Mirrors src/cli/commands/completions.ts (simplified — the TS reference
// generates richer dynamic completion; the Go port emits a minimal version
// pinned to the verb set).
func RunCompletions(opts CompletionsOptions) (int, error) {
	var script string
	switch opts.Shell {
	case "bash":
		script = bashScript
	case "zsh":
		script = zshScript
	case "fish":
		script = fishScript
	default:
		return 1, userErrorf("unsupported shell %q", opts.Shell)
	}
	if opts.Output.JSONMode() {
		opts.Output.JSON(struct {
			Shell  string `json:"shell"`
			Script string `json:"script"`
		}{Shell: opts.Shell, Script: script})
		return 0, nil
	}
	opts.Output.Print(script)
	return 0, nil
}

const bashScript = `# c3p bash completion. Source this file or eval its output:
#   eval "$(c3p completions bash)"
_c3p_complete() {
  local cur=${COMP_WORDS[COMP_CWORD]}
  local prev=${COMP_WORDS[COMP_CWORD-1]}
  local verbs="init list use status drift diff new validate sync hook doctor completions help"
  case "$prev" in
    use|diff|validate)
      if [ -d ".claude-profiles" ]; then
        COMPREPLY=( $(compgen -W "$(ls .claude-profiles 2>/dev/null | grep -v '^\\.\\|^_')" -- "$cur") )
        return 0
      fi
      ;;
    hook)
      COMPREPLY=( $(compgen -W "install uninstall" -- "$cur") )
      return 0
      ;;
    completions)
      COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
      return 0
      ;;
  esac
  COMPREPLY=( $(compgen -W "$verbs" -- "$cur") )
}
complete -F _c3p_complete c3p
`

const zshScript = `# c3p zsh completion. Save as ~/.zfunc/_c3p (or eval).
#compdef c3p
_c3p() {
  local -a verbs
  verbs=(init list use status drift diff new validate sync hook doctor completions help)
  _arguments -C \
    '1: :->verb' \
    '*: :->args'
  case $state in
    verb) _describe 'command' verbs ;;
    args)
      case $words[2] in
        use|diff|validate)
          if [ -d .claude-profiles ]; then
            local -a profiles
            profiles=(${(f)"$(ls .claude-profiles 2>/dev/null | grep -v '^\.\|^_')"})
            _describe 'profile' profiles
          fi
          ;;
        hook) _values 'action' install uninstall ;;
        completions) _values 'shell' bash zsh fish ;;
      esac
      ;;
  esac
}
_c3p
`

const fishScript = `# c3p fish completion. Save to ~/.config/fish/completions/c3p.fish
function __c3p_profiles
  if test -d .claude-profiles
    ls .claude-profiles 2>/dev/null | grep -v '^\.\|^_'
  end
end
complete -c c3p -n '__fish_use_subcommand' -a 'init list use status drift diff new validate sync hook doctor completions help'
complete -c c3p -n '__fish_seen_subcommand_from use diff validate' -a '(__c3p_profiles)'
complete -c c3p -n '__fish_seen_subcommand_from hook' -a 'install uninstall'
complete -c c3p -n '__fish_seen_subcommand_from completions' -a 'bash zsh fish'
complete -c c3p -l json
complete -c c3p -l quiet -s q
complete -c c3p -l no-color
complete -c c3p -l help -s h
complete -c c3p -l version -s V
`
