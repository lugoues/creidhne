package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
)

// subsumedByQuadlet are the [Unit] directives that Quadlet's own auto-generated
// resource dependency already covers, so hand-writing one at a resource the
// container also references is redundant. Deliberately narrow: BindsTo=/PartOf=/
// Conflicts= etc. change semantics and may be intentional, so they are not
// flagged.
var subsumedByQuadlet = map[string]bool{"After": true, "Requires": true, "Wants": true}

// resourceKeyword maps a resource edge's relationship to the [Container] key
// that induced it, for a human-readable message.
func resourceKeyword(rel string) string {
	switch rel {
	case "image":
		return "Image"
	case "pod":
		return "Pod"
	case "network":
		return "Network"
	case "volume":
		return "Volume"
	}
	return ""
}

// networkOnlineTargets are the units Quadlet wires implicitly (root vs rootless);
// naming them by hand in After=/Wants= is redundant.
var networkOnlineTargets = map[string]bool{
	"network-online.target":                   true,
	"podman-user-wait-network-online.service": true,
}

type lintFinding struct {
	Unit    string
	Message string
}

func newLintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint [quadlet...]",
		Short: "Report [Unit] dependencies that Quadlet already wires for you",
		Long: "lint flags redundant [Unit] dependency declarations:\n\n" +
			"  - After=/Requires=/Wants= pointing at a .network/.volume/.pod/.build/\n" +
			"    .image the same container already references (Network=, Volume=, Pod=,\n" +
			"    Image=). Quadlet emits that dependency from the resource reference, so\n" +
			"    the hand-written one is duplicated.\n" +
			"  - After=/Wants= on network-online.target (or the rootless\n" +
			"    podman-user-wait-network-online.service). Quadlet adds these to every\n" +
			"    generated unit by default.\n\n" +
			"It also enforces whole-project graph contracts that no single quadlet\n" +
			"can express: pair-network cardinality (creidhne.pair marker), duplicate\n" +
			"effective runtime names, orphan networks, duplicate traefik routers.\n\n" +
			"With no arguments it lints the whole project; given quadlet names it lints\n" +
			"only those. Exits non-zero when it finds anything.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			all, err := loadQuadlets(cfg.ProjectDir)
			if err != nil {
				return err
			}
			if len(all) == 0 {
				return fmt.Errorf("no quadlets found (no top-level #Quadlet values in %s)", cfg.ProjectDir)
			}
			focus := all
			if len(args) > 0 {
				focus, err = filterQuadlets(all, args)
				if err != nil {
					return err
				}
			}
			findings := lintQuadlets(focus, all)
			rules := graphRuleFindings(all)
			if len(args) > 0 {
				rules = focusRuleFindings(rules, focus)
			}
			out := cmd.OutOrStdout()
			if len(findings) == 0 && len(rules) == 0 {
				fmt.Fprintln(out, "lint: no redundant dependencies found")
				return nil
			}
			printLintFindings(out, findings)
			if len(rules) > 0 {
				if len(findings) > 0 {
					fmt.Fprintln(out)
				}
				printRuleFindings(out, rules)
			}
			fmt.Fprintf(out, "\n%d finding(s)\n", len(findings)+len(rules))
			return errSilent{}
		},
	}
	return cmd
}

// lintQuadlets collects redundant-dependency findings for the focus quadlets,
// resolving references against the whole project.
func lintQuadlets(focus, all []eval.Quadlet) []lintFinding {
	var findings []lintFinding

	// Redundant resource dependencies: the graph already resolves both the
	// resource reference and any hand-written [Unit] dep to the same node, so a
	// merged edge that carries both a resource rel and a subsumed directive is
	// exactly a redundant declaration.
	for _, e := range buildGraph(focus, all).mergedEdges() {
		resource := ""
		var dirs []string
		for _, r := range e.Rels {
			if k := resourceKeyword(r); k != "" {
				resource = k
			} else if subsumedByQuadlet[r] {
				dirs = append(dirs, r+"=")
			}
		}
		if resource == "" || len(dirs) == 0 {
			continue
		}
		findings = append(findings, lintFinding{
			Unit:    e.From,
			Message: fmt.Sprintf("%s on %s is redundant with the %s= reference — Quadlet wires this dependency automatically", strings.Join(dirs, "/"), e.To, resource),
		})
	}

	// Redundant network-online dependencies. [Quadlet] DefaultDependencies=false
	// disables Quadlet's implicit network-online deps, so a hand-written one is
	// then required, not redundant -- skip those units.
	for _, q := range focus {
		for _, u := range q.Units {
			if v, ok := nestedBool(u.Data, "Quadlet", "DefaultDependencies"); ok && !v {
				continue
			}
			for _, dir := range []string{"After", "Wants"} {
				for _, t := range nestedList(u.Data, "Unit", dir) {
					if networkOnlineTargets[t] {
						findings = append(findings, lintFinding{
							Unit:    u.Filename,
							Message: fmt.Sprintf("%s=%s is redundant — Quadlet adds network-online dependencies to every generated unit by default", dir, t),
						})
					}
				}
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Unit != findings[j].Unit {
			return findings[i].Unit < findings[j].Unit
		}
		return findings[i].Message < findings[j].Message
	})
	return findings
}

func printLintFindings(out io.Writer, findings []lintFinding) {
	cur := ""
	for _, f := range findings {
		if f.Unit != cur {
			cur = f.Unit
			fmt.Fprintln(out, cur)
		}
		fmt.Fprintln(out, "  "+yellow("redundant:")+" "+f.Message)
	}
}

// focusRuleFindings keeps findings attributed to units of the focus quadlets.
func focusRuleFindings(fs []ruleFinding, focus []eval.Quadlet) []ruleFinding {
	in := map[string]bool{}
	for _, q := range focus {
		for _, u := range q.Units {
			in[u.Filename] = true
		}
	}
	out := fs[:0:0]
	for _, f := range fs {
		if in[f.Unit] {
			out = append(out, f)
		}
	}
	return out
}
