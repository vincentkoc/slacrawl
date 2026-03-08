package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	commandNames = []string{
		"init",
		"doctor",
		"sync",
		"tail",
		"watch",
		"search",
		"messages",
		"mentions",
		"sql",
		"users",
		"channels",
		"status",
		"completion",
	}
	globalFlags = []string{
		"--config",
		"--format",
		"--json",
		"--no-color",
		"--help",
		"-h",
	}
	commandFlags = map[string][]string{
		"init":       {"--workspace", "--db", "--help", "-h"},
		"doctor":     {"--help", "-h"},
		"sync":       {"--source", "--workspace", "--channels", "--since", "--full", "--concurrency", "--help", "-h"},
		"tail":       {"--workspace", "--repair-every", "--help", "-h"},
		"watch":      {"--desktop-every", "--help", "-h"},
		"search":     {"--workspace", "--help", "-h"},
		"messages":   {"--workspace", "--channel", "--author", "--limit", "--help", "-h"},
		"mentions":   {"--workspace", "--target", "--limit", "--help", "-h"},
		"sql":        {"--help", "-h"},
		"users":      {"--workspace", "--help", "-h"},
		"channels":   {"--workspace", "--help", "-h"},
		"status":     {"--help", "-h"},
		"completion": {"--help", "-h"},
	}
)

func (a *App) runCompletion(args []string) error {
	if len(args) == 0 {
		return errors.New("completion shell required: bash or zsh")
	}
	shell := strings.ToLower(strings.TrimSpace(args[0]))
	switch shell {
	case "bash":
		_, err := io.WriteString(a.Stdout, bashCompletionScript())
		return err
	case "zsh":
		_, err := io.WriteString(a.Stdout, zshCompletionScript())
		return err
	default:
		return fmt.Errorf("unsupported completion shell %q: use bash or zsh", shell)
	}
}

func bashCompletionScript() string {
	commands := strings.Join(commandNames, " ")
	global := strings.Join(globalFlags, " ")
	var b strings.Builder
	b.WriteString(`# bash completion for slacrawl
_slacrawl()
{
    local cur prev words cword
    _init_completion || return

    local commands="`)
	b.WriteString(commands)
	b.WriteString(`"
    local global_flags="`)
	b.WriteString(global)
	b.WriteString(`"

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands} ${global_flags}" -- "${cur}") )
        return
    fi

    local command=""
    local i
    for ((i=1; i < ${#words[@]}; i++)); do
        case "${words[i]}" in
            init|doctor|sync|tail|watch|search|messages|mentions|sql|users|channels|status|completion)
                command="${words[i]}"
                break
                ;;
        esac
    done

    case "${prev}" in
        --format)
            COMPREPLY=( $(compgen -W "text json log" -- "${cur}") )
            return
            ;;
        --source)
            COMPREPLY=( $(compgen -W "api desktop all" -- "${cur}") )
            return
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh" -- "${cur}") )
            return
            ;;
    esac

    case "${command}" in
        init)
            COMPREPLY=( $(compgen -W "--workspace --db --help -h ${global_flags}" -- "${cur}") )
            ;;
        doctor)
            COMPREPLY=( $(compgen -W "--help -h ${global_flags}" -- "${cur}") )
            ;;
        sync)
            COMPREPLY=( $(compgen -W "--source --workspace --channels --since --full --concurrency --help -h ${global_flags}" -- "${cur}") )
            ;;
        tail)
            COMPREPLY=( $(compgen -W "--workspace --repair-every --help -h ${global_flags}" -- "${cur}") )
            ;;
        watch)
            COMPREPLY=( $(compgen -W "--desktop-every --help -h ${global_flags}" -- "${cur}") )
            ;;
        search)
            COMPREPLY=( $(compgen -W "--workspace --help -h ${global_flags}" -- "${cur}") )
            ;;
        messages)
            COMPREPLY=( $(compgen -W "--workspace --channel --author --limit --help -h ${global_flags}" -- "${cur}") )
            ;;
        mentions)
            COMPREPLY=( $(compgen -W "--workspace --target --limit --help -h ${global_flags}" -- "${cur}") )
            ;;
        users)
            COMPREPLY=( $(compgen -W "--workspace --help -h ${global_flags}" -- "${cur}") )
            ;;
        channels)
            COMPREPLY=( $(compgen -W "--workspace --help -h ${global_flags}" -- "${cur}") )
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh --help -h ${global_flags}" -- "${cur}") )
            ;;
        *)
            COMPREPLY=( $(compgen -W "${global_flags}" -- "${cur}") )
            ;;
    esac
}

complete -F _slacrawl slacrawl
`)
	return b.String()
}

func zshCompletionScript() string {
	commands := make([]string, 0, len(commandNames))
	for _, name := range commandNames {
		commands = append(commands, fmt.Sprintf(`"%s:%s command"`, name, name))
	}
	var b strings.Builder
	b.WriteString(`#compdef slacrawl

_slacrawl() {
  local -a commands
  commands=(
    `)
	b.WriteString(strings.Join(commands, "\n    "))
	b.WriteString(`
  )

  _arguments -C \
    '--config[override config path]:path:_files' \
    '--format[output format]:format:(text json log)' \
    '--json[compatibility alias for json output]' \
    '--no-color[disable ANSI color in text output]' \
    '1:command:->command' \
    '*::arg:->args'

  case $state in
    command)
      _describe 'command' commands
      ;;
    args)
      case $words[2] in
        init)
          _arguments '--workspace[workspace id]:workspace id:' '--db[database path]:database path:_files'
          ;;
        sync)
          _arguments '--source[sync source]:source:(api desktop all)' '--workspace[workspace id]:workspace id:' '--channels[channel ids]:channels:' '--since[start timestamp]:timestamp:' '--full[run full sync]' '--concurrency[worker count]:count:'
          ;;
        tail)
          _arguments '--workspace[workspace id]:workspace id:' '--repair-every[repair interval]:duration:'
          ;;
        watch)
          _arguments '--desktop-every[desktop refresh interval]:duration:'
          ;;
        search)
          _arguments '--workspace[workspace id]:workspace id:'
          ;;
        messages)
          _arguments '--workspace[workspace id]:workspace id:' '--channel[channel id]:channel id:' '--author[user id]:user id:' '--limit[row limit]:limit:'
          ;;
        mentions)
          _arguments '--workspace[workspace id]:workspace id:' '--target[target id or label]:target:' '--limit[row limit]:limit:'
          ;;
        users)
          _arguments '--workspace[workspace id]:workspace id:'
          ;;
        channels)
          _arguments '--workspace[workspace id]:workspace id:'
          ;;
        completion)
          _values 'shell' bash zsh
          ;;
      esac
      ;;
  esac
}

_slacrawl "$@"
`)
	return b.String()
}
