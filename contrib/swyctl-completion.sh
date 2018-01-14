_swyctl()
{
	local cmd cur prev words cword split=false

	if type -t _init_completion >/dev/null; then
		_init_completion -n = || return
	else
		# manual initialization for older bash completion versions
		COMPREPLY=()
		cur="${COMP_WORDS[COMP_CWORD]}"
		prev="${COMP_WORDS[COMP_CWORD-1]}"
	fi

	if [ $COMP_CWORD -gt 1 ] ; then
		cmd="${COMP_WORDS[1]}"
	else
		cmd="$prev"
	fi

	case "$cmd" in
	add)
		COMPREPLY=($(compgen -W '-event -lang -mw -proj -rl -src -tmo' -- $cur))
		return
		;;
	upd)
		COMPREPLY=($(compgen -W '-mw -proj -rl -src -tmo' -- $cur))
		return
		;;
	code)
		COMPREPLY=($(compgen -W '-proj -version' -- $cur))
		return
		;;
	uadd)
		COMPREPLY=($(compgen -W '-name -pass' -- $cur))
		return
		;;
	pass)
		COMPREPLY=($(compgen -W '-pass' -- $cur))
		return
		;;
	ls|inf|run|del|logs|on|off|mls|madd|mdel)
		COMPREPLY=($(compgen -W '-proj' -- $cur))
		return
		;;
	*swyctl*)
		COMPREPLY=($(compgen -W 'login ps ls inf add run upd del logs code on off mls madd mdel uls uadd udel pass uinf mt lng' -- $cur))
		return
		;;
	esac

	return
} &&
complete -F _swyctl swyctl
complete -F _swyctl ./swyctl
