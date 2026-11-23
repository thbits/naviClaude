package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"
)

// runList prints every live Claude session, as a table or a JSON array.
func runList(args []string) int {
	fs := flag.NewFlagSet("naviclaude list", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: naviclaude list [--json]")
		fmt.Fprintln(os.Stderr, "\nList live Claude sessions in tmux with their status (waiting, working, done, idle).")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	asJSON := fs.Bool("json", false, "print a JSON array")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if len(pos) > 0 {
		fs.Usage()
		return 2
	}
	agents, err := newLiveSource(true).Agents()
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude list:", err)
		return 1
	}
	if *asJSON {
		err = writeAgentsJSON(os.Stdout, agents)
	} else {
		err = writeAgentsTable(os.Stdout, agents, time.Now())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "naviclaude list:", err)
		return 1
	}
	return 0
}

// writeAgentsJSON writes agents as an indented JSON array ("[]" when empty).
func writeAgentsJSON(w io.Writer, agents []agentInfo) error {
	if agents == nil {
		agents = []agentInfo{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(agents)
}

// writeAgentsTable writes an aligned table; "*" marks the caller's own session.
func writeAgentsTable(w io.Writer, agents []agentInfo, now time.Time) error {
	if len(agents) == 0 {
		_, err := fmt.Fprintln(w, "No live Claude sessions.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PANE\tTARGET\tNAME\tSTATUS\tPROJECT\tBRANCH\tMODEL\tLAST\tID")
	for _, a := range agents {
		pane := a.PaneID
		if a.Self {
			pane += "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pane, a.Target, a.Name, a.statusLabel(), dash(a.Project), dash(a.Branch),
			dash(a.Model), agoText(a.LastActivity, now), shortID(a.ID))
	}
	return tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// shortID keeps enough of a UUID to be a unique prefix in practice.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return dash(id)
}
