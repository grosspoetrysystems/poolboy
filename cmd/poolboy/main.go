// Command poolboy is a standalone CLI for Poolboy bundles (OKF markdown).
package main

import (
	"fmt"
	"os"
	"strings"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "dev"

// rootDir is the bundle to operate on, set by a leading --root flag. Empty
// means discover from the current directory.
var rootDir string

const usage = `Poolboy: maintain a documentation corpus

Usage: poolboy [--root <dir>] <command> [flags]

Commands:
  init          scaffold a documentation project (--force)
  build         render and publish the configured corpus
  scan          record a source inventory (--accept to replace baseline)
  drift         compare the source inventory with current files
  affected      find documents citing a source resource
  status        corpus counts: entries, links, tags, checkboxes, broken, orphans
  list, ls      list entries (--where key=value --prefix --sort=path|timestamp --reverse)
  read          print an entry's body (frontmatter stripped)
  outline       print an entry's heading hierarchy
  table         extract a dataset's markdown table as csv/json (--n)
  search        full-text search over entries (every word; --any --exact --where --prefix --lines)
  checkboxes    list open checklist items; optional [file] scopes to one entry (--all --done --prefix --where)
  tags          list tags in use (--counts --sort=name|count --prefix --where)
  properties    list frontmatter keys in use (--counts --sort --prefix --where)
  property      list values of a frontmatter key (--counts --sort --prefix --where)
  unresolved    broken internal links
  orphans       entries with no incoming links
  links         an entry's outgoing links
  backlinks     every link that points to an entry
  move, mv      relocate or rename an entry, rewriting links (--dry-run, --include-frontmatter)
  tidy          canonicalize the corpus: --links, --slug, --wikilinks, --all (bare = preview)
  check         report conformance + health issues (--fix repairs safe ones)
  version       print the version

Run 'poolboy <command> -h' to see a command's exact flags
  --root <dir>      operate on the project at <dir> (default: discover from cwd)

Every command that reports results accepts --format text|json|csv|tsv (default text; csv/tsv suit
  list-shaped results); version prints a bare string
Two filters, available wherever a set of entries is narrowed (list, search, checkboxes, tags,
properties, property): --prefix <path> for where, --where key=value or key!=value for what
  (repeatable = AND). type and tags are ordinary fields, e.g. --where type=note, --where status!=done
list --format json carries each entry's full frontmatter; csv/tsv carry the canonical columns
Exit codes: 0 ok, 1 no match or check errors, 2 error
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	args, code := applyRoot(args)
	if code != 0 {
		return code
	}
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch cmd := args[0]; cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	case "init":
		return cmdInit(args[1:])
	case "version", "--version", "-v":
		fmt.Println("poolboy", Version)
		return 0
	case "build":
		return cmdBuild(args[1:])
	case "scan":
		return cmdScan(args[1:])
	case "drift":
		return cmdDrift(args[1:])
	case "affected":
		return cmdAffected(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "list", "ls":
		return cmdList(args[1:])
	case "read":
		return cmdRead(args[1:])
	case "outline":
		return cmdOutline(args[1:])
	case "search":
		return cmdSearch(args[1:])
	case "checkboxes":
		return cmdCheckboxes(args[1:])
	case "table":
		return cmdTable(args[1:])
	case "tags":
		return cmdTags(args[1:])
	case "properties":
		return cmdProperties(args[1:])
	case "property":
		return cmdProperty(args[1:])
	case "unresolved":
		return cmdUnresolved(args[1:])
	case "orphans":
		return cmdOrphans(args[1:])
	case "links":
		return cmdLinks(args[1:])
	case "backlinks":
		return cmdBacklinks(args[1:])
	case "move", "mv":
		return cmdMove(args[1:])
	case "tidy":
		return cmdTidy(args[1:])
	case "check":
		return cmdCheck(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", cmd, usage)
		return 2
	}
}

// applyRoot consumes a leading --root <dir> option, recording the bundle to
// operate on (no chdir), and returns the remaining args. Unlike git's -C it
// does not change the working directory: it only redirects bundle discovery,
// so commands that don't open a bundle (notably init) are unaffected.
func applyRoot(args []string) ([]string, int) {
	rootDir = ""
	for len(args) > 0 {
		switch {
		case args[0] == "--root":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "poolboy: --root needs a directory")
				return nil, 2
			}
			rootDir, args = args[1], args[2:]
		case strings.HasPrefix(args[0], "--root="):
			rootDir, args = args[0][len("--root="):], args[1:]
		default:
			return args, 0
		}
	}
	return args, 0
}
