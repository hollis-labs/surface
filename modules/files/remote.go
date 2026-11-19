package files

// Remote places: folders on another host, reached with the system ssh client, so the
// owner's ssh config (host aliases, keys, agent, jump hosts) applies as it does in a
// terminal. Every operation is one fixed POSIX shell script (remoteScript) run on the
// host with its arguments as positional parameters: the command line ssh sends is built
// only from quoted words, and nothing the owner or the agent typed is ever read by a shell
// as code.
//
// The checks are the local ones. A path is cleaned here (cleanRel) and refused when it
// looks like a credential (sensitive) before anything is sent. The host resolves it to a
// real path, refuses one that leads out of the root through a link, and reports the real
// path back, which is checked against the denylist again before any content is read or
// written. An operation that changes something then names the real path it was told, and
// the host refuses to act if the path resolves anywhere else by then.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Remote is one remote place as the files.remotes setting gives it.
type Remote struct {
	// ID is a slug; the place's id is "remote:<id>". Empty: made from host and path.
	ID string `json:"id"`
	// Label is the place's name. Empty: "host:path".
	Label string `json:"label"`
	// Host is an ssh destination: a Host alias from the owner's ssh config, or user@host.
	Host string `json:"host"`
	// Path is the folder on the host: absolute, or "~" or "~/..." for the remote home.
	Path string `json:"path"`
}

// remotePrefix starts the place id of every remote place.
const remotePrefix = "remote:"

// The bounds of remote operations.
const (
	// remoteTimeout bounds one ssh call; remoteGrepTimeout a search.
	remoteTimeout     = 30 * time.Second
	remoteGrepTimeout = 90 * time.Second
	// remoteListBytes bounds a listing as it comes over the wire.
	remoteListBytes = 4 << 20
	// remoteRawLimit is the largest file sent whole (an image in the preview, a download).
	remoteRawLimit = 16 << 20
	// remoteGrepBytes bounds a search's answer as it comes over the wire.
	remoteGrepBytes = 2 << 20
)

var (
	// errRemote marks a host that could not be reached or could not do what was asked.
	errRemote = errors.New("remote")
	// hostOK is what an ssh destination may look like: no option, no space, no shell.
	hostOK = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._@-]*$`)
)

func isRemote(place string) bool { return strings.HasPrefix(place, remotePrefix) }

// parseRemotes reads the files.remotes setting: a JSON list of Remote. Empty is none.
func parseRemotes(s string) ([]Remote, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var rs []Remote
	if err := json.Unmarshal([]byte(s), &rs); err != nil {
		return nil, fmt.Errorf("the files.remotes setting is not a JSON list of {id, label, host, path}: %w", err)
	}
	seen := map[string]bool{}
	for i := range rs {
		r := &rs[i]
		r.Host, r.Path = strings.TrimSpace(r.Host), strings.TrimSpace(r.Path)
		if !hostOK.MatchString(r.Host) {
			return nil, fmt.Errorf("files.remotes: %q is not an ssh host; give a Host alias from your ssh config or user@host", r.Host)
		}
		if r.Path != "~" && !strings.HasPrefix(r.Path, "~/") && !strings.HasPrefix(r.Path, "/") {
			return nil, fmt.Errorf("files.remotes: the path %q on %s must be absolute or start with ~/", r.Path, r.Host)
		}
		if strings.ContainsAny(r.Path, "\x00\n") {
			return nil, fmt.Errorf("files.remotes: the path on %s has a control character", r.Host)
		}
		if sensitive(r.Path) {
			return nil, fmt.Errorf("%w: %s:%s cannot be a place", errSensitive, r.Host, r.Path)
		}
		if r.ID == "" {
			r.ID = r.Host + "-" + strings.TrimPrefix(r.Path, "~")
		}
		r.ID = strings.Trim(notSlug.ReplaceAllString(strings.ToLower(r.ID), "-"), "-")
		if r.ID == "" {
			return nil, fmt.Errorf("files.remotes: the remote on %s needs an id", r.Host)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("files.remotes: two remotes are called %q", r.ID)
		}
		seen[r.ID] = true
		if r.Label == "" {
			r.Label = r.Host + ":" + r.Path
		}
	}
	return rs, nil
}

func (r Remote) place() Place {
	return Place{ID: remotePrefix + r.ID, Name: r.Label, Path: r.Path, Host: r.Host, Remote: true}
}

// runner runs argv on a host. The real one is ssh; tests use a fake, or run the script on
// this machine. out holds at most limit bytes of the command's output; truncated says
// there was more.
type runner interface {
	run(ctx context.Context, host string, argv []string, stdin []byte, limit int) (res runResult, err error)
}

type runResult struct {
	out       []byte
	stderr    string
	code      int
	truncated bool
}

// shQuote quotes one word for the remote shell ssh hands the command line to.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// commandLine is the one string ssh sends: every word quoted.
func commandLine(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shQuote(a)
	}
	return strings.Join(q, " ")
}

// sshArgs is the ssh client's argv for running argv on host. BatchMode keeps ssh from
// ever asking for a password or a host key on a terminal no one is at.
func sshArgs(host string, argv []string) []string {
	return []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"--", host, commandLine(argv),
	}
}

// sshRunner is the real runner: the system ssh client.
type sshRunner struct{}

// sshPath finds ssh even when the app is started without a login PATH.
func sshPath() string {
	if p, err := exec.LookPath("ssh"); err == nil {
		return p
	}
	return "/usr/bin/ssh"
}

func (sshRunner) run(ctx context.Context, host string, argv []string, stdin []byte, limit int) (runResult, error) {
	if !hostOK.MatchString(host) {
		return runResult{}, fmt.Errorf("%w: %q is not an ssh host", errRemote, host)
	}
	cmd := exec.CommandContext(ctx, sshPath(), sshArgs(host, argv)...) //nolint:gosec // the host is checked; every remote word is quoted
	return execute(cmd, stdin, limit)
}

// execute runs a command with bounded output.
func execute(cmd *exec.Cmd, stdin []byte, limit int) (runResult, error) {
	out := &capped{max: limit}
	errOut := &capped{max: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, errOut
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	err := cmd.Run()
	res := runResult{out: out.buf.Bytes(), stderr: errOut.buf.String(), truncated: out.over}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.code = ee.ExitCode()
		return res, nil
	}
	return res, err
}

// capped keeps the first max bytes written to it and drops the rest, so a command that
// says too much costs no more memory than max.
type capped struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room < len(p) {
		c.over = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

// remoteScript does every remote operation. It is fixed text: what varies arrives as its
// positional parameters ($1 the operation, $2 the root as configured, $3 the path relative
// to it, cleaned, $4 "1" to follow a final link, $5 the real path a change expects, empty
// to skip that check), then the operation's own. Exit codes, which remoteError reads:
//
//	3 the root is not a folder there    4 the path leads out of the root
//	5 not there, or a broken link        6 a folder where a file is needed, or the reverse
//	7 already exists                     8 a folder that is not empty
//	9 not a regular file                 10 the path resolves elsewhere than it did
//	11 too large                         12 binary, so not replaced
//	13 the destination's folder is not there   14 moving a folder into itself
//	120 the host lacks GNU find or coreutils
const remoteScript = `set -u
unset CDPATH
op=$1 base=$2 rel=$3 follow=$4 want=$5
shift 5
case $base in "~") base=$HOME ;; "~/"*) base=$HOME/${base#"~/"} ;; esac
B=$(cd -P -- "$base" 2>/dev/null && pwd -P) || exit 3
# rp prints the real path of $1, which need not exist: missing parts are kept as written
# after the nearest folder that does.
rp() {
	p=$1 t=
	while [ ! -e "$p" ]; do
		[ -L "$p" ] && return 5
		t=/${p##*/}$t
		p=${p%/*}
		[ -n "$p" ] || p=/
	done
	if [ -d "$p" ]; then
		r=$(cd -P -- "$p" && pwd -P) || return 5
	elif [ -L "$p" ]; then
		r=$(realpath -- "$p" 2>/dev/null || readlink -f -- "$p") || return 120
	else
		d=${p%/*}
		[ -n "$d" ] || d=/
		r=$(cd -P -- "$d" && pwd -P) || return 5
		r=${r%/}/${p##*/}
	fi
	[ "$r" = / ] && [ -n "$t" ] && r=
	printf '%s\n' "$r$t"
}
inside() {
	[ "$B" = / ] && return 0
	case $1 in "$B" | "$B"/*) return 0 ;; esac
	return 1
}
# at sets T to the real path of a path relative to the root.
at() {
	if [ -z "$1" ]; then
		T=$B
	elif [ "$2" = 1 ]; then
		T=$(rp "$B/$1") || exit $?
	else
		T=$B/$1
		T=$(rp "${T%/*}") || exit $?
		T=${T%/}/${1##*/}
	fi
	inside "$T" || exit 4
}
at "$rel" "$follow"
[ -z "$want" ] || [ "$T" = "$want" ] || exit 10
nuls() { head -c 8000 -- "$1" | tr -dc '\000' | wc -c | tr -d ' '; }
case $op in
stat)
	if [ "$follow" != 1 ] && [ -L "$T" ]; then k=l
	elif [ -d "$T" ]; then k=d
	elif [ -f "$T" ]; then k=f
	elif [ -e "$T" ]; then k=o
	else k=m
	fi
	s=0 n=0
	if [ "$k" = f ]; then
		s=$(wc -c <"$T" | tr -d ' ')
		n=$(nuls "$T")
	fi
	printf '%s\t%s\t%s\t%s' "$k" "$s" "$n" "$T"
	;;
list)
	[ -e "$T" ] || exit 5
	[ -d "$T" ] || exit 6
	find / -maxdepth 0 -printf '' 2>/dev/null || exit 120
	printf '%s\0' "$T"
	find "$T/" -mindepth 1 -maxdepth 1 -printf '%y\t%Y\t%s\t%T@\t%f\0' 2>/dev/null | head -c "$1"
	;;
read)
	[ -e "$T" ] || exit 5
	[ -d "$T" ] && exit 6
	[ -f "$T" ] || exit 9
	s=$(wc -c <"$T" | tr -d ' ')
	n=$(nuls "$T")
	printf '%s %s\n' "$s" "$n"
	[ "$n" = 0 ] || exit 0
	tail -n "+$1" -- "$T" | head -c "$2"
	;;
raw)
	[ -e "$T" ] || exit 5
	[ -f "$T" ] || exit 9
	[ "$(wc -c <"$T" | tr -d ' ')" -le "$1" ] || exit 11
	cat -- "$T"
	;;
grep)
	[ -e "$T" ] || exit 5
	depth=$1 files=$2 limit=$3 mode=$4 icase=$5 glob=$6 q=$7
	case $mode in F | E) ;; *) exit 2 ;; esac
	head -z -n 1 </dev/null 2>/dev/null || exit 120
	walk() {
		find "$T" -maxdepth "$depth" \( -type d \( -name .git -o -name .hg -o -name .svn -o -name node_modules \
			-o -name .venv -o -name __pycache__ -o -name .cache -o -iname .ssh -o -iname .gnupg \) -prune \) \
			-o \( -type f -size -1025k "$@" ! -iname '*.pem' ! -iname '*.key' ! -iname '*.kdbx' ! -iname '*.p12' \
			! -iname '*.pfx' ! -iname 'id_rsa*' ! -iname 'id_dsa*' ! -iname 'id_ecdsa*' ! -iname 'id_ed25519*' \
			! -iname .env ! -iname '.env.*' ! -iname .netrc ! -iname .pgpass ! -iname .npmrc ! -iname .pypirc -print0 \)
	}
	search() {
		xargs -0 sh -c 'm=$1 i=$2 q=$3; shift 3; printf "%s\n" "$#" >&2
			if [ "$i" = 1 ]; then exec grep -HnIi --null "-$m" -e "$q" -- "$@"; fi
			exec grep -HnI --null "-$m" -e "$q" -- "$@"' sh "$mode" "$icase" "$q"
	}
	{
		if [ -n "$glob" ]; then walk -name "$glob"; else walk; fi 2>/dev/null |
			head -z -n "$files" | search 2>&1 >&3 3>&- | grep -E '^[0-9]+$' >&2
	} 3>&1 | head -n "$limit"
	;;
write)
	createonly=$1 max=$2
	[ -d "$T" ] && exit 6
	if [ -e "$T" ]; then
		[ "$createonly" = 1 ] && exit 7
		[ -f "$T" ] || exit 9
		[ "$(nuls "$T")" = 0 ] || exit 12
		perm=$(stat -c %a -- "$T" 2>/dev/null || stat -f %Lp -- "$T") || exit 120
		tmp=$(mktemp "${T%/*}/.${T##*/}.XXXXXX") || exit 1
		if head -c "$max" >"$tmp" && chmod "$perm" "$tmp" && mv -f -- "$tmp" "$T"; then
			printf replaced
		else
			rm -f -- "$tmp"
			exit 1
		fi
	else
		mkdir -p -- "${T%/*}" || exit 1
		(set -C && head -c "$max" >"$T") 2>/dev/null || { [ -e "$T" ] && exit 7; exit 1; }
		printf created
	fi
	;;
mkdir)
	{ [ -e "$T" ] || [ -L "$T" ]; } && exit 7
	mkdir -p -- "$T"
	;;
move)
	F=$T
	at "$1" 0
	[ "$T" = "$2" ] || exit 10
	{ [ -e "$F" ] || [ -L "$F" ]; } || exit 5
	{ [ -e "$T" ] || [ -L "$T" ]; } && exit 7
	[ -d "${T%/*}" ] || exit 13
	case $T in "$F"/*) exit 14 ;; esac
	mv -- "$F" "$T"
	;;
delete)
	if [ -L "$T" ] || [ -f "$T" ]; then
		rm -f -- "$T"
	elif [ -d "$T" ]; then
		[ -z "$(ls -A -- "$T")" ] || exit 8
		rmdir -- "$T"
	elif [ -e "$T" ]; then
		exit 9
	else
		exit 5
	fi
	;;
*) exit 2 ;;
esac
`

// remote is a remote place with the runner that reaches it.
type remote struct {
	cfg Remote
	run runner
}

// rloc is a path in a remote place, cleaned and checked here, and Abs, its real path on
// the host once the host has resolved it.
type rloc struct {
	r   remote
	Rel string
	Abs string
}

func (l rloc) name() string {
	if l.Rel == "" {
		return l.r.cfg.Label
	}
	return path.Base(l.Rel)
}

// call runs one operation of remoteScript.
func (r remote) call(ctx context.Context, op, rel string, follow bool, want string, stdin []byte, limit int, extra ...string) (runResult, error) {
	timeout := remoteTimeout
	if op == "grep" {
		timeout = remoteGrepTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	f := "0"
	if follow {
		f = "1"
	}
	argv := append([]string{"sh", "-c", remoteScript, "sh", op, r.cfg.Path, rel, f, want}, extra...)
	res, err := r.run.run(ctx, r.cfg.Host, argv, stdin, limit)
	if err != nil {
		if ctx.Err() != nil {
			return res, fmt.Errorf("%w: %s did not answer in time", errRemote, r.cfg.Host)
		}
		return res, fmt.Errorf("%w: could not run ssh to %s: %w", errRemote, r.cfg.Host, err)
	}
	return res, r.failure(res, rel)
}

// failure turns an exit code of remoteScript (or of ssh) into an error in the owner's
// terms.
func (r remote) failure(res runResult, rel string) error {
	what := rel
	if what == "" {
		what = r.cfg.Label
	}
	switch res.code {
	case 0:
		return nil
	case 3:
		return fmt.Errorf("%w: the folder of remote place %q (%s:%s) is not there", errNotFound, r.cfg.ID, r.cfg.Host, r.cfg.Path)
	case 4:
		return fmt.Errorf("%w: %q leads out of %s through a link", errOutside, rel, r.cfg.Label)
	case 5:
		return fmt.Errorf("%w: %s is not there", errNotFound, what)
	case 6:
		return fmt.Errorf("%s is a folder where a file is needed, or a file where a folder is", what)
	case 7:
		return fmt.Errorf("%w: %s; pick another name", errExists, what)
	case 8:
		return fmt.Errorf("%s is not empty; delete what is in it first", what)
	case 9:
		return fmt.Errorf("%s is not a regular file", what)
	case 10:
		return fmt.Errorf("%s changed on %s while it was being checked; try again", what, r.cfg.Host)
	case 11:
		return fmt.Errorf("%s is larger than %d MB, too large to send whole", what, remoteRawLimit>>20)
	case 12:
		return fmt.Errorf("%s was left as it is: %w, and Files only replaces text", what, errBinary)
	case 13:
		return fmt.Errorf("%w: the destination's folder is not there; create it first", errNotFound)
	case 14:
		return fmt.Errorf("%s cannot be moved into itself", what)
	case 120, 126, 127:
		return fmt.Errorf("%w: %s lacks a tool Files needs there (GNU find and coreutils, as any Linux has)", errRemote, r.cfg.Host)
	case 255:
		return fmt.Errorf("%w: could not reach %s over ssh: %s", errRemote, r.cfg.Host, lastLine(res.stderr))
	}
	return fmt.Errorf("%w: %s failed on %s (exit %d): %s", errRemote, what, r.cfg.Host, res.code, lastLine(res.stderr))
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return "no message"
	}
	return clip(s, 200)
}

// check cleans a path and refuses it when it looks like a credential, before the host is
// asked anything.
func (r remote) check(rel string) (string, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	if sensitive(rel) {
		return "", fmt.Errorf("%w (%s)", errSensitive, rel)
	}
	return rel, nil
}

// guard refuses a real path on the host that looks like a credential: a link can lead
// to one from an ordinary name.
func (l rloc) guard() error {
	if sensitive(l.Abs) {
		return fmt.Errorf("%w (%s)", errSensitive, l.Rel)
	}
	return nil
}

// rstat is what the host says of a path.
type rstat struct {
	rloc
	// Kind is d (folder), f (regular file), l (a link, not followed), o (something else)
	// or m (missing).
	Kind   byte
	Size   int64
	Binary bool
}

// stat resolves and checks a path on the host.
func (r remote) stat(ctx context.Context, rel string, follow bool) (rstat, error) {
	rel, err := r.check(rel)
	if err != nil {
		return rstat{}, err
	}
	res, err := r.call(ctx, "stat", rel, follow, "", nil, 64<<10)
	if err != nil {
		return rstat{}, err
	}
	f := strings.SplitN(string(res.out), "\t", 4)
	if len(f) != 4 || len(f[0]) != 1 || !strings.HasPrefix(f[3], "/") {
		return rstat{}, fmt.Errorf("%w: %s gave an answer Files cannot read", errRemote, r.cfg.Host)
	}
	s := rstat{rloc: rloc{r: r, Rel: rel, Abs: f[3]}, Kind: f[0][0]}
	s.Size, _ = strconv.ParseInt(f[1], 10, 64)
	s.Binary = f[2] != "0"
	return s, s.guard()
}

// list returns a remote folder's entries, sorted as a local listing is.
func (r remote) list(ctx context.Context, rel string, showHidden bool) ([]Entry, error) {
	rel, err := r.check(rel)
	if err != nil {
		return nil, err
	}
	res, err := r.call(ctx, "list", rel, true, "", nil, remoteListBytes+4096, strconv.Itoa(remoteListBytes))
	if err != nil {
		return nil, err
	}
	head, body, ok := bytes.Cut(res.out, []byte{0})
	if !ok {
		return nil, fmt.Errorf("%w: %s gave an answer Files cannot read", errRemote, r.cfg.Host)
	}
	l := rloc{r: r, Rel: rel, Abs: string(head)}
	if err := l.guard(); err != nil {
		return nil, err
	}
	return parseListing(l, body, showHidden), nil
}

// parseListing reads the records of find -printf '%y\t%Y\t%s\t%T@\t%f\0'. A record cut
// short by the byte limit is dropped.
func parseListing(l rloc, body []byte, showHidden bool) []Entry {
	recs := bytes.Split(body, []byte{0})
	if len(recs) > 0 {
		recs = recs[:len(recs)-1] // what follows the last NUL is empty, or cut short
	}
	out := make([]Entry, 0, len(recs))
	for _, rec := range recs {
		f := strings.SplitN(string(rec), "\t", 5)
		if len(f) != 5 || f[4] == "" || strings.Contains(f[4], "/") {
			continue
		}
		name := f[4]
		if !showHidden && hidden(name) {
			continue
		}
		e := Entry{Name: name, Path: path.Join(l.Rel, name), Symlink: f[0] == "l", Dir: f[1] == "d"}
		if !e.Dir {
			e.Size, _ = strconv.ParseInt(f[2], 10, 64)
		}
		if sec, err := strconv.ParseFloat(f[3], 64); err == nil {
			e.Modified = time.Unix(int64(sec), 0).UTC().Format(time.RFC3339)
		}
		e.Locked = sensitive(e.Path) || sensitive(l.Abs+"/"+name)
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// readText reads lines of a remote text file, with the bounds of a local read.
func (r remote) readText(ctx context.Context, rel string, offset, limit int, maxBytes int64) (Text, error) {
	s, err := r.stat(ctx, rel, true)
	if err != nil {
		return Text{}, err
	}
	switch s.Kind {
	case 'm':
		return Text{}, fmt.Errorf("%w: %s is not there", errNotFound, s.name())
	case 'd':
		return Text{}, fmt.Errorf("%s is a folder; list it instead", s.name())
	case 'f':
	default:
		return Text{}, fmt.Errorf("%s is not a regular file", s.name())
	}
	if s.Binary {
		return Text{}, fmt.Errorf("%s cannot be shown as text: %w", s.name(), errBinary)
	}
	if offset < 1 {
		offset = 1
	}
	// One byte past the limit tells a file that goes on from one that ends there.
	res, err := s.r.call(ctx, "read", s.Rel, true, s.Abs, nil, int(maxBytes)+256,
		strconv.Itoa(offset), strconv.FormatInt(maxBytes+1, 10))
	if err != nil {
		return Text{}, err
	}
	head, body, ok := bytes.Cut(res.out, []byte{'\n'})
	var size, nul int64
	if _, serr := fmt.Sscanf(string(head), "%d %d", &size, &nul); !ok || serr != nil {
		return Text{}, fmt.Errorf("%w: %s gave an answer Files cannot read", errRemote, r.cfg.Host)
	}
	if nul > 0 {
		return Text{}, fmt.Errorf("%s cannot be shown as text: %w", s.name(), errBinary)
	}
	t, err := scanText(bufio.NewReader(bytes.NewReader(body)), offset, offset, limit, maxBytes)
	t.Size = size
	return t, err
}

// raw returns a remote file's bytes, up to remoteRawLimit.
func (r remote) raw(ctx context.Context, rel string) (rstat, []byte, error) {
	s, err := r.stat(ctx, rel, true)
	if err != nil {
		return s, nil, err
	}
	if s.Kind != 'f' {
		return s, nil, fmt.Errorf("%s is not a file that can be sent", s.name())
	}
	res, err := s.r.call(ctx, "raw", s.Rel, true, s.Abs, nil, remoteRawLimit, strconv.Itoa(remoteRawLimit))
	if err != nil {
		return s, nil, err
	}
	if res.truncated {
		return s, nil, fmt.Errorf("%s is larger than %d MB, too large to send whole", s.name(), remoteRawLimit>>20)
	}
	return s, res.out, nil
}

// grep searches the text files under a remote folder with the host's own find and grep.
// The bounds are the local search's. A regular expression is checked as RE2 here and run
// as a POSIX extended one there; (?i) at its start makes it ignore case.
func (r remote) grep(ctx context.Context, rel string, q grepQuery) (grepResult, error) {
	res := grepResult{Matches: []Match{}}
	if q.Query == "" {
		return res, errors.New("say what to search for")
	}
	if strings.ContainsAny(q.Query, "\x00\n") {
		return res, errors.New("search for one line of text at a time")
	}
	mode, icase, query := "F", "1", q.Query
	if q.Regexp {
		if _, err := regexp.Compile(q.Query); err != nil {
			return res, fmt.Errorf("the regular expression does not compile; fix it, or search for it as plain text: %w", err)
		}
		mode, icase = "E", "0"
		if rest, ok := strings.CutPrefix(query, "(?i)"); ok {
			icase, query = "1", rest
		}
	}
	if q.Glob != "" {
		if _, err := path.Match(q.Glob, ""); err != nil {
			return res, fmt.Errorf("the glob %q is malformed; use a file name pattern like *.md", q.Glob)
		}
	}
	if q.MaxDepth <= 0 {
		q.MaxDepth = grepMaxDepth
	}
	if q.MaxMatches <= 0 {
		q.MaxMatches = grepMaxMatches
	}
	q.MaxMatches = min(q.MaxMatches, grepMatchCap)

	s, err := r.stat(ctx, rel, true)
	if err != nil {
		return res, err
	}
	if s.Kind == 'm' {
		return res, fmt.Errorf("%w: %s is not there", errNotFound, s.name())
	}
	out, err := s.r.call(ctx, "grep", s.Rel, true, s.Abs, nil, remoteGrepBytes,
		strconv.Itoa(q.MaxDepth+1), strconv.Itoa(grepMaxFiles), strconv.Itoa(q.MaxMatches+1), mode, icase, q.Glob, query)
	if err != nil {
		return res, err
	}
	for _, line := range strings.Split(out.stderr, "\n") {
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			res.Files += n
		}
	}
	res.Matches, res.Truncated = parseGrep(s.rloc, out.out, q.MaxMatches)
	res.Truncated = res.Truncated || out.truncated || res.Files >= grepMaxFiles
	return res, nil
}

// parseGrep reads grep -Hn --null output ("path\0line:text\n") under a resolved start:
// paths become relative to the place, and any that look like credentials are dropped.
func parseGrep(start rloc, out []byte, limit int) (matches []Match, truncated bool) {
	matches = []Match{}
	for rec := range bytes.SplitSeq(out, []byte{'\n'}) {
		p, rest, ok := bytes.Cut(rec, []byte{0})
		if !ok {
			continue
		}
		num, text, ok := bytes.Cut(rest, []byte{':'})
		if !ok {
			continue
		}
		n, err := strconv.Atoi(string(num))
		if err != nil {
			continue
		}
		abs := string(p)
		rel := start.Rel
		if abs != start.Abs {
			sub, ok := strings.CutPrefix(abs, strings.TrimSuffix(start.Abs, "/")+"/")
			if !ok {
				continue
			}
			rel = path.Join(start.Rel, sub)
		}
		if sensitive(abs) || sensitive(rel) {
			continue
		}
		if len(matches) >= limit {
			return matches, true
		}
		matches = append(matches, Match{Path: rel, Line: n, Text: clip(strings.TrimSpace(strings.ToValidUTF8(string(text), "�")), grepLineClip)})
	}
	return matches, false
}

// writeText creates or replaces a remote text file, as writeText does a local one.
func (r remote) writeText(ctx context.Context, rel, text string, createOnly bool) (rloc, bool, error) {
	if len(text) > maxBody {
		return rloc{}, false, fmt.Errorf("the text is over %d MB; Files does not write a file that large", maxBody>>20)
	}
	s, err := r.stat(ctx, rel, true)
	if err != nil {
		return s.rloc, false, err
	}
	if s.Rel == "" {
		return s.rloc, false, errors.New("that is the place itself; give the path of a file in it")
	}
	co := "0"
	if createOnly {
		co = "1"
	}
	res, err := s.r.call(ctx, "write", s.Rel, true, s.Abs, []byte(text), 64, co, strconv.Itoa(len(text)))
	if err != nil {
		return s.rloc, false, err
	}
	return s.rloc, string(res.out) == "created", nil
}

// change runs an operation that changes something at a path without following a final
// link: mkdir and delete.
func (r remote) change(ctx context.Context, op, rel string) (rloc, error) {
	s, err := r.stat(ctx, rel, false)
	if err != nil {
		return s.rloc, err
	}
	if s.Rel == "" {
		if op == "delete" {
			return s.rloc, errors.New("a place's own folder is never deleted; take it out of files.remotes instead")
		}
		return s.rloc, errors.New("say what to call the new folder")
	}
	_, err = s.r.call(ctx, op, s.Rel, false, s.Abs, nil, 4096)
	return s.rloc, err
}

// move renames or moves something inside a remote place.
func (r remote) move(ctx context.Context, from, to string) (rloc, rloc, error) {
	src, err := r.stat(ctx, from, false)
	if err != nil {
		return src.rloc, rloc{}, err
	}
	dst, err := r.stat(ctx, to, false)
	if err != nil {
		return src.rloc, dst.rloc, err
	}
	if src.Rel == "" || dst.Rel == "" {
		return src.rloc, dst.rloc, errors.New("a place itself cannot be moved or replaced")
	}
	_, err = r.call(ctx, "move", src.Rel, false, src.Abs, nil, 4096, dst.Rel, dst.Abs)
	return src.rloc, dst.rloc, err
}
