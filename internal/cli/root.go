package cli

import (
	"agt/internal/app"
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type rootOptions struct {
	Repo string
	JSON bool
}

func Execute() error {
	opts := &rootOptions{}
	rootCmd := &cobra.Command{
		Use:           "agt",
		Short:         "AgentGit CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().StringVar(&opts.Repo, "repo", ".", "target git repository")
	rootCmd.PersistentFlags().BoolVar(&opts.JSON, "json", false, "machine-readable output")

	rootCmd.AddCommand(
		newCreateCmd(opts),
		newSnapshotCmd(opts),
		newRollbackCmd(opts),
		newDoneCmd(opts),
		newAbortCmd(opts),
		newListCmd(opts),
		newStatusCmd(opts),
		newDiffCmd(opts),
		newStopCmd(opts),
		newCleanCmd(opts),
		newDashCmd(opts),
	)

	return rootCmd.Execute()
}

func newCreateCmd(opts *rootOptions) *cobra.Command {
	var purpose string
	var owner string
	var path string
	var from string
	var sparse []string

	cmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Create an agent worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			summary, err := svc.Create(app.CreateOptions{
				ID:      args[0],
				Purpose: purpose,
				Owner:   owner,
				Path:    path,
				From:    from,
				Sparse:  sparse,
			})
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(summary)
			}
			fmt.Printf("%s %s %s\n", summary.ID, summary.Path, summary.Branch)
			return nil
		},
	}

	cmd.Flags().StringVar(&purpose, "purpose", "", "task description")
	cmd.Flags().StringVar(&owner, "owner", "", "agent owner name")
	cmd.Flags().StringVar(&path, "path", "", "worktree path")
	cmd.Flags().StringVar(&from, "from", "", "start revision")
	cmd.Flags().StringArrayVar(&sparse, "sparse", nil, "sparse checkout pattern")
	return cmd
}

func newSnapshotCmd(opts *rootOptions) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "snapshot <id>",
		Short: "Create a snapshot commit for an agent worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			result, err := svc.Snapshot(args[0], message)
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(result)
			}
			if !result.Created {
				fmt.Printf("%s no changes\n", result.ID)
				return nil
			}
			fmt.Printf("%s %s\n", result.Snapshot.Name, result.Commit)
			return nil
		},
	}
	cmd.Flags().StringVar(&message, "msg", "", "snapshot message")
	return cmd
}

func newRollbackCmd(opts *rootOptions) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "rollback <id> <target>",
		Short: "Rollback an agent worktree to a snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			result, err := svc.Rollback(args[0], args[1], reason)
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(result)
			}
			fmt.Printf("%s %s\n", result.ID, result.Commit)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "rollback reason")
	return cmd
}

func newDoneCmd(opts *rootOptions) *cobra.Command {
	var msg string
	var authorName string
	var authorEmail string
	cmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Finalize agent work and clean up worktree state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			result, err := svc.Done(args[0], app.DoneOptions{
				Message:     msg,
				AuthorName:  authorName,
				AuthorEmail: authorEmail,
			})
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(result)
			}
			if result.Commit == "" {
				fmt.Printf("%s cleaned %s\n", result.ID, result.Branch)
				return nil
			}
			fmt.Printf("%s %s %s\n", result.ID, result.Branch, result.Commit)
			return nil
		},
	}
	cmd.Flags().StringVar(&msg, "msg", "", "final commit message")
	cmd.Flags().StringVar(&authorName, "author-name", "", "final commit author name")
	cmd.Flags().StringVar(&authorEmail, "author-email", "", "final commit author email")
	return cmd
}

func newAbortCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "abort <id>",
		Short: "Abort agent work and remove worktree, refs, and branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			result, err := svc.Abort(args[0])
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(result)
			}
			fmt.Printf("%s aborted\n", result.ID)
			return nil
		},
	}
	return cmd
}

func newListCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all agent worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			summaries, err := svc.ListAgents()
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(summaries)
			}
			if len(summaries) == 0 {
				fmt.Println("no agent worktrees")
				return nil
			}
			for _, summary := range summaries {
				owner := summary.Owner
				if owner == "" {
					owner = "-"
				}
				purpose := summary.Purpose
				if purpose == "" {
					purpose = "-"
				}
				fmt.Printf("%-14s %-9s owner=%-12s snaps=%-3d +%-4d -%-4d %s\n",
					summary.ID,
					summary.Status,
					owner,
					summary.Snapshots,
					summary.DiffStat.Insertions,
					summary.DiffStat.Deletions,
					purpose,
				)
				fmt.Printf("  path=%s\n", summary.Path)
				if summary.LastActivity != "" {
					fmt.Printf("  last_activity=%s\n", summary.LastActivity)
				}
			}
			return nil
		},
	}
	return cmd
}

func newStatusCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Show detailed status for an agent worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			status, err := svc.Status(args[0])
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(status)
			}
			fmt.Printf("%s %s snaps=%d locked=%t\n", status.Summary.ID, status.Summary.Status, status.Summary.Snapshots, status.Locked)
			fmt.Printf("path: %s\n", status.Summary.Path)
			fmt.Printf("branch: %s\n", status.Summary.Branch)
			if status.Summary.Purpose != "" {
				fmt.Printf("purpose: %s\n", status.Summary.Purpose)
			}
			if status.Summary.Owner != "" {
				fmt.Printf("owner: %s\n", status.Summary.Owner)
			}
			if status.Base != "" {
				fmt.Printf("base: %s\n", status.Base)
			}
			if status.Latest != "" {
				fmt.Printf("latest: %s\n", status.Latest)
			}
			if status.Stop != nil {
				fmt.Printf("stop: %s (%s)\n", status.Stop.Reason, status.Stop.CreatedAt)
			}
			fmt.Printf("diff: %d files, +%d -%d\n",
				status.Summary.DiffStat.Files,
				status.Summary.DiffStat.Insertions,
				status.Summary.DiffStat.Deletions,
			)
			if len(status.CurrentChanges) > 0 {
				fmt.Println("\ncurrent changes")
				for _, change := range status.CurrentChanges {
					fmt.Printf("  %-4s %s\n", change.Status, change.Path)
				}
			}
			for _, snapshot := range status.Snapshots {
				fmt.Printf("\n%s %s %s\n", snapshot.Name, formatTimestamp(snapshot.Timestamp), snapshot.Commit)
				fmt.Printf("  message: %s\n", snapshot.Message)
				for _, change := range snapshot.Changes {
					fmt.Printf("  %-4s %s\n", change.Status, change.Path)
				}
			}
			return nil
		},
	}
	return cmd
}

func newDiffCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <id> [left] [right]",
		Short: "Diff snapshots or current worktree state",
		Args:  cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}

			left := ""
			right := ""
			if len(args) == 1 {
				left = "latest"
				right = "current"
			}
			if len(args) >= 2 {
				left = args[1]
				right = "current"
			}
			if len(args) == 3 {
				right = args[2]
			}

			diff, err := svc.Diff(args[0], left, right)
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(map[string]string{"diff": diff})
			}
			fmt.Println(diff)
			return nil
		},
	}
	return cmd
}

func newStopCmd(opts *rootOptions) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "stop <id>",
		Short: "Write a cooperative stop signal and lock the worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			status, err := svc.Stop(args[0], reason)
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(status)
			}
			fmt.Printf("%s stopped\n", status.Summary.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "stop reason")
	return cmd
}

func newCleanCmd(opts *rootOptions) *cobra.Command {
	var hours float64
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove stale orphaned worktrees and refs",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			candidates, err := svc.CleanCandidates(hours)
			if err != nil {
				return err
			}
			if opts.JSON && dryRun {
				return printJSON(candidates)
			}
			if len(candidates) == 0 {
				if opts.JSON {
					return printJSON(app.CleanResult{})
				}
				fmt.Println("no clean candidates")
				return nil
			}
			if dryRun {
				for _, candidate := range candidates {
					fmt.Printf("%s %s %s\n", candidate.Kind, candidate.ID, candidate.Reason)
				}
				return nil
			}

			selected := candidates
			if !force {
				selected, err = promptCleanSelection(candidates)
				if err != nil {
					return err
				}
				if len(selected) == 0 {
					if opts.JSON {
						return printJSON(app.CleanResult{})
					}
					fmt.Println("nothing selected")
					return nil
				}
			}

			result, err := svc.ApplyClean(selected)
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(result)
			}
			for _, removed := range result.Removed {
				fmt.Printf("removed %s %s\n", removed.Kind, removed.ID)
			}
			return nil
		},
	}
	cmd.Flags().Float64Var(&hours, "hours", 24, "stale threshold in hours")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show candidates without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "remove all candidates without prompting")
	return cmd
}

func newDashCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "dash",
		Short: "Open the agent dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.NewService(opts.Repo)
			if err != nil {
				return err
			}
			return runDashboard(svc)
		},
	}
}

func promptCleanSelection(candidates []app.CleanCandidate) ([]app.CleanCandidate, error) {
	reader := bufio.NewReader(os.Stdin)
	selected := make([]app.CleanCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		fmt.Printf("clean %s %s (%s)? [y/N]: ", candidate.Kind, candidate.ID, candidate.Reason)
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "y" || answer == "yes" {
			selected = append(selected, candidate)
		}
	}
	return selected, nil
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func formatTimestamp(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
