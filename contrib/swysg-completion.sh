_swysg()
{
	local cur prev words cword split=false

	if type -t _init_completion >/dev/null; then
		_init_completion -n = || return
	else
		# manual initialization for older bash completion versions
		COMPREPLY=()
		cur="${COMP_WORDS[COMP_CWORD]}"
		prev="${COMP_WORDS[COMP_CWORD-1]}"
	fi

	case "$prev" in
	*swysg*)
		COMPREPLY=($(compgen -W '-b -l -n' -- $cur))
		return
		;;
	esac
} &&
complete -F _swysg swysg
complete -F _swysg ./swysg
