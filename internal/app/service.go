package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var filesChangedPattern = regexp.MustCompile(`(\d+)\s+files?\s+changed`)
var insertionsPattern = regexp.MustCompile(`(\d+)\s+insertions?\(\+\)`)
var deletionsPattern = regexp.MustCompile(`(\d+)\s+deletions?\(-\)`)

type Service struct {
	Repo   string
	Config Config
}

type CreateOptions struct {
	ID      string
	Purpose string
	Owner   string
	Path    string
	From    string
	Sparse  []string
}

type DoneOptions struct {
	Message     string
	AuthorName  string
	AuthorEmail string
}

type AgentMeta struct {
	ID        string `json:"id"`
	Purpose   string `json:"purpose,omitempty"`
	Owner     string `json:"owner,omitempty"`
	Branch    string `json:"branch"`
	Path      string `json:"path"`
	Repo      string `json:"repo"`
	CreatedAt string `json:"created_at"`
}

type StopSignal struct {
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"created_at"`
}

type DiffStat struct {
	Files      int `json:"files"`
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

type FileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type SnapshotInfo struct {
	Name      string       `json:"name"`
	Commit    string       `json:"commit"`
	Parent    string       `json:"parent,omitempty"`
	Timestamp string       `json:"timestamp"`
	Message   string       `json:"message"`
	Changes   []FileChange `json:"changes,omitempty"`
}

type AgentSummary struct {
	ID           string   `json:"id"`
	Path         string   `json:"path,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Purpose      string   `json:"purpose,omitempty"`
	Owner        string   `json:"owner,omitempty"`
	Status       string   `json:"status"`
	Snapshots    int      `json:"snapshots"`
	DiffStat     DiffStat `json:"diff_stat"`
	LastActivity string   `json:"last_activity,omitempty"`
}

type AgentStatus struct {
	Summary        AgentSummary   `json:"summary"`
	Base           string         `json:"base,omitempty"`
	Latest         string         `json:"latest,omitempty"`
	Locked         bool           `json:"locked"`
	Stop           *StopSignal    `json:"stop,omitempty"`
	CurrentChanges []FileChange   `json:"current_changes,omitempty"`
	Snapshots      []SnapshotInfo `json:"snapshots,omitempty"`
}

type SnapshotResult struct {
	ID       string        `json:"id"`
	Created  bool          `json:"created"`
	Commit   string        `json:"commit,omitempty"`
	Snapshot *SnapshotInfo `json:"snapshot,omitempty"`
}

type RollbackResult struct {
	ID     string `json:"id"`
	Commit string `json:"commit"`
}

type ActionResult struct {
	ID      string `json:"id"`
	Branch  string `json:"branch,omitempty"`
	Path    string `json:"path,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Message string `json:"message,omitempty"`
}

type CleanCandidate struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Path         string `json:"path,omitempty"`
	Branch       string `json:"branch,omitempty"`
	Reason       string `json:"reason"`
	LastActivity string `json:"last_activity,omitempty"`
}

type CleanResult struct {
	Removed []CleanCandidate `json:"removed"`
}

type WorktreeInfo struct {
	Path         string
	Branch       string
	Head         string
	Locked       bool
	LockedReason string
	Main         bool
}

type agentState struct {
	ID       string
	Worktree *WorktreeInfo
	Meta     *AgentMeta
	Base     string
	Latest   string
	Stop     *StopSignal
}

func NewService(path string) (*Service, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is required but was not found in PATH")
	}

	if path == "" {
		path = "."
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("git", "-C", absPath, "rev-parse", "--show-toplevel")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("failed to resolve git repo from %s: %s", absPath, msg)
	}

	repo := strings.TrimSpace(stdout.String())
	cfg, err := loadConfig(repo)
	if err != nil {
		return nil, err
	}

	return &Service{Repo: repo, Config: cfg}, nil
}

func (s *Service) Create(opts CreateOptions) (_ *AgentSummary, err error) {
	if !agentIDPattern.MatchString(opts.ID) {
		return nil, fmt.Errorf("invalid agent id %q: only letters, numbers, ., _, - are allowed", opts.ID)
	}

	if existing, _, err := s.readRef(s.metaRef(opts.ID)); err != nil {
		return nil, err
	} else if existing != "" {
		return nil, fmt.Errorf("agent %q already exists", opts.ID)
	}

	worktrees, err := s.listWorktrees()
	if err != nil {
		return nil, err
	}
	if _, ok := worktrees[opts.ID]; ok {
		return nil, fmt.Errorf("worktree for %q already exists", opts.ID)
	}

	baseSpec := opts.From
	if baseSpec == "" {
		baseSpec = "HEAD"
	}
	baseCommit, err := s.git("", nil, "", "rev-parse", baseSpec)
	if err != nil {
		return nil, err
	}

	branch := fmt.Sprintf("agent/%s", opts.ID)
	if out, err := s.git("", nil, "", "for-each-ref", "--format=%(refname)", "refs/heads/"+branch); err != nil {
		return nil, err
	} else if strings.TrimSpace(out) != "" {
		return nil, fmt.Errorf("branch %q already exists; choose a new agent id or delete the preserved branch", branch)
	}

	worktreePath := opts.Path
	if worktreePath == "" {
		worktreePath = filepath.Join(s.Repo, ".worktrees", opts.ID)
	}
	if !filepath.IsAbs(worktreePath) {
		worktreePath = filepath.Join(s.Repo, worktreePath)
	}
	worktreePath = filepath.Clean(worktreePath)
	if filepath.Base(worktreePath) != opts.ID {
		return nil, fmt.Errorf("worktree path basename %q must match agent id %q", filepath.Base(worktreePath), opts.ID)
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return nil, err
	}

	defer func() {
		if err == nil {
			return
		}
		_ = s.unlockWorktree(worktreePath)
		_ = s.removeWorktree(worktreePath)
		_ = s.deleteBranch(branch)
		_ = s.deleteAgentRefs(opts.ID)
	}()

	if _, err = s.git("", nil, "", "worktree", "add", "-b", branch, worktreePath, baseCommit); err != nil {
		return nil, err
	}

	if len(opts.Sparse) > 0 {
		if _, err = s.git(worktreePath, nil, "", "sparse-checkout", "init", "--cone"); err != nil {
			return nil, err
		}
		args := append([]string{"sparse-checkout", "set"}, opts.Sparse...)
		if _, err = s.git(worktreePath, nil, "", args...); err != nil {
			return nil, err
		}
	}

	meta := AgentMeta{
		ID:        opts.ID,
		Purpose:   opts.Purpose,
		Owner:     firstNonEmpty(opts.Owner, s.Config.DefaultOwner),
		Branch:    branch,
		Path:      worktreePath,
		Repo:      s.Repo,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err = s.writeJSONRef(s.metaRef(opts.ID), meta); err != nil {
		return nil, err
	}
	if err = s.updateRef(s.baseRef(opts.ID), baseCommit); err != nil {
		return nil, err
	}

	return s.Summary(opts.ID)
}

func (s *Service) Snapshot(id, message string) (*SnapshotResult, error) {
	state, err := s.loadState(id)
	if err != nil {
		return nil, err
	}
	if state.Worktree == nil {
		return nil, fmt.Errorf("agent %q has no active worktree", id)
	}

	statusOutput, err := s.git(state.Worktree.Path, nil, "", "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(statusOutput) == "" {
		return &SnapshotResult{ID: id, Created: false}, nil
	}

	parent := state.Latest
	if parent == "" {
		parent = state.Base
	}
	if parent == "" {
		return nil, fmt.Errorf("agent %q is missing a base commit", id)
	}

	treeID, err := s.buildTreeFromWorktree(id, state.Worktree.Path, parent)
	if err != nil {
		return nil, err
	}
	parentTree, err := s.treeForCommit(parent)
	if err != nil {
		return nil, err
	}
	if treeID == parentTree {
		return &SnapshotResult{ID: id, Created: false}, nil
	}

	if strings.TrimSpace(message) == "" {
		message = s.renderTemplate(s.Config.SnapshotMessageFormat, id, state.Meta, time.Now().UTC())
	}
	commitID, err := s.createCommit(treeID, parent, message, s.authorEnv(state.metaOwnerOrDefault(), ""))
	if err != nil {
		return nil, err
	}
	if err := s.updateRef(s.latestRef(id), commitID); err != nil {
		return nil, err
	}

	snapshot, err := s.snapshotInfo(commitID, "")
	if err != nil {
		return nil, err
	}
	count, err := s.snapshotCount(&agentState{Base: state.Base, Latest: commitID})
	if err != nil {
		return nil, err
	}
	snapshot.Name = fmt.Sprintf("snap-%d", count)
	return &SnapshotResult{ID: id, Created: true, Commit: commitID, Snapshot: snapshot}, nil
}

func (s *Service) Rollback(id, spec, reason string) (*RollbackResult, error) {
	state, err := s.loadState(id)
	if err != nil {
		return nil, err
	}
	if state.Worktree == nil {
		return nil, fmt.Errorf("agent %q has no active worktree", id)
	}

	target, err := s.resolveSnapshotSpec(id, spec)
	if err != nil {
		return nil, err
	}

	lockReason := reason
	if strings.TrimSpace(lockReason) == "" {
		lockReason = "rollback " + spec
	}
	if err := s.lockWorktree(state.Worktree.Path, lockReason); err != nil {
		return nil, err
	}

	if _, err := s.git(state.Worktree.Path, nil, "", "restore", "--source="+target, "--staged", "--worktree", "--", "."); err != nil {
		return nil, err
	}
	if _, err := s.git(state.Worktree.Path, nil, "", "clean", "-fd"); err != nil {
		return nil, err
	}
	if target == state.Base {
		if err := s.deleteRefIfExists(s.latestRef(id)); err != nil {
			return nil, err
		}
	} else {
		if err := s.updateRef(s.latestRef(id), target); err != nil {
			return nil, err
		}
	}

	return &RollbackResult{ID: id, Commit: target}, nil
}

func (s *Service) Stop(id, reason string) (*AgentStatus, error) {
	if strings.TrimSpace(reason) == "" {
		reason = s.Config.DefaultStopReason
	}

	state, err := s.loadState(id)
	if err != nil {
		return nil, err
	}

	stop := StopSignal{
		Reason:    reason,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.writeJSONRef(s.stopRef(id), stop); err != nil {
		return nil, err
	}
	if state.Worktree != nil {
		if err := s.lockWorktree(state.Worktree.Path, reason); err != nil {
			return nil, err
		}
	}

	return s.Status(id)
}

func (s *Service) Done(id string, opts DoneOptions) (*ActionResult, error) {
	state, err := s.loadState(id)
	if err != nil {
		return nil, err
	}
	if state.Worktree == nil {
		return nil, fmt.Errorf("agent %q has no active worktree", id)
	}

	statusOutput, err := s.git(state.Worktree.Path, nil, "", "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}

	message := strings.TrimSpace(opts.Message)
	if message == "" {
		message = s.defaultDoneMessage(id, state.Meta)
	}

	branchParent, err := s.git(state.Worktree.Path, nil, "", "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}

	finalTree := ""
	if strings.TrimSpace(statusOutput) != "" {
		parentForTree := state.Latest
		if parentForTree == "" {
			parentForTree = branchParent
		}
		finalTree, err = s.buildTreeFromWorktree(id, state.Worktree.Path, parentForTree)
		if err != nil {
			return nil, err
		}
	} else if state.Latest != "" {
		finalTree, err = s.treeForCommit(state.Latest)
		if err != nil {
			return nil, err
		}
	}

	var finalCommit string
	if finalTree != "" {
		parentTree, err := s.treeForCommit(branchParent)
		if err != nil {
			return nil, err
		}
		if finalTree != parentTree {
			env := s.authorEnv(s.authorName(opts, state.Meta), s.authorEmail(opts))
			finalCommit, err = s.createCommit(finalTree, branchParent, message, env)
			if err != nil {
				return nil, err
			}
			if _, err := s.git(state.Worktree.Path, nil, "", "reset", "--hard", finalCommit); err != nil {
				return nil, err
			}
		}
	}

	if err := s.unlockWorktree(state.Worktree.Path); err != nil {
		return nil, err
	}
	if err := s.removeWorktree(state.Worktree.Path); err != nil {
		return nil, err
	}
	if err := s.deleteAgentRefs(id); err != nil {
		return nil, err
	}

	branch := state.branchOrDefault(id)
	return &ActionResult{ID: id, Branch: branch, Commit: finalCommit, Message: message}, nil
}

func (s *Service) Abort(id string) (*ActionResult, error) {
	state, err := s.loadState(id)
	if err != nil {
		return nil, err
	}

	branch := state.branchOrDefault(id)
	if state.Worktree != nil {
		if err := s.unlockWorktree(state.Worktree.Path); err != nil {
			return nil, err
		}
		if err := s.removeWorktree(state.Worktree.Path); err != nil {
			return nil, err
		}
	}
	if err := s.deleteBranch(branch); err != nil {
		return nil, err
	}
	if err := s.deleteAgentRefs(id); err != nil {
		return nil, err
	}

	return &ActionResult{ID: id, Branch: branch, Message: "aborted"}, nil
}

func (s *Service) ListAgents() ([]AgentSummary, error) {
	worktrees, err := s.listWorktrees()
	if err != nil {
		return nil, err
	}
	ids, err := s.agentIDs()
	if err != nil {
		return nil, err
	}
	for id := range worktrees {
		ids[id] = struct{}{}
	}

	summaries := make([]AgentSummary, 0, len(ids))
	for id := range ids {
		summary, err := s.Summary(id)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		if statusWeight(summaries[i].Status) != statusWeight(summaries[j].Status) {
			return statusWeight(summaries[i].Status) < statusWeight(summaries[j].Status)
		}
		return summaries[i].ID < summaries[j].ID
	})
	return summaries, nil
}

func (s *Service) Summary(id string) (*AgentSummary, error) {
	state, err := s.loadState(id)
	if err != nil {
		return nil, err
	}

	snapshots, err := s.snapshotCount(state)
	if err != nil {
		return nil, err
	}
	diffStat, err := s.diffStat(state)
	if err != nil {
		return nil, err
	}

	lastActivity, err := s.lastActivity(state)
	if err != nil {
		return nil, err
	}

	return &AgentSummary{
		ID:           id,
		Path:         state.pathOrEmpty(),
		Branch:       state.branchOrDefault(id),
		Purpose:      state.purposeOrEmpty(),
		Owner:        state.metaOwnerOrDefault(),
		Status:       state.status(),
		Snapshots:    snapshots,
		DiffStat:     diffStat,
		LastActivity: lastActivity,
	}, nil
}

func (s *Service) Status(id string) (*AgentStatus, error) {
	state, err := s.loadState(id)
	if err != nil {
		return nil, err
	}

	summary, err := s.Summary(id)
	if err != nil {
		return nil, err
	}
	snapshots, err := s.snapshots(state)
	if err != nil {
		return nil, err
	}
	currentChanges, err := s.currentChanges(state)
	if err != nil {
		return nil, err
	}

	return &AgentStatus{
		Summary:        *summary,
		Base:           state.Base,
		Latest:         state.Latest,
		Locked:         state.worktreeLocked(),
		Stop:           state.Stop,
		CurrentChanges: currentChanges,
		Snapshots:      snapshots,
	}, nil
}

func (s *Service) Diff(id, left, right string) (string, error) {
	state, err := s.loadState(id)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(left) == "" && strings.TrimSpace(right) == "" && state.Latest == "" && state.Base != "" {
		left = "base"
		right = "current"
	}

	leftResolved, leftCurrent, err := s.resolveDiffSide(id, left)
	if err != nil {
		return "", err
	}
	rightResolved, rightCurrent, err := s.resolveDiffSide(id, right)
	if err != nil {
		return "", err
	}

	switch {
	case leftCurrent && rightCurrent:
		return "", errors.New("diff between current and current is empty")
	case leftCurrent:
		if state.Worktree == nil {
			return "", fmt.Errorf("agent %q has no active worktree", id)
		}
		currentTree, err := s.buildTreeFromWorktree(id, state.Worktree.Path, rightResolved)
		if err != nil {
			return "", err
		}
		return s.git("", nil, "", "diff", currentTree, rightResolved, "--")
	case rightCurrent:
		if state.Worktree == nil {
			return "", fmt.Errorf("agent %q has no active worktree", id)
		}
		currentTree, err := s.buildTreeFromWorktree(id, state.Worktree.Path, leftResolved)
		if err != nil {
			return "", err
		}
		return s.git("", nil, "", "diff", leftResolved, currentTree, "--")
	default:
		return s.git("", nil, "", "diff", leftResolved, rightResolved, "--")
	}
}

func (s *Service) CleanCandidates(hours float64) ([]CleanCandidate, error) {
	worktrees, err := s.listWorktrees()
	if err != nil {
		return nil, err
	}
	refIDs, err := s.agentIDs()
	if err != nil {
		return nil, err
	}

	if hours <= 0 {
		hours = s.Config.CleanThresholdHours
	}
	threshold := time.Duration(hours * float64(time.Hour))
	now := time.Now()
	candidates := make([]CleanCandidate, 0)

	for id, wt := range worktrees {
		if wt.Main {
			continue
		}
		if _, ok := refIDs[id]; ok {
			continue
		}
		activity, err := latestFileModTime(wt.Path)
		if err != nil {
			return nil, err
		}
		if activity.IsZero() {
			if info, statErr := os.Stat(wt.Path); statErr == nil {
				activity = info.ModTime()
			}
		}
		if !activity.IsZero() && now.Sub(activity) < threshold {
			continue
		}
		lastActivity := ""
		if !activity.IsZero() {
			lastActivity = activity.UTC().Format(time.RFC3339)
		}
		candidates = append(candidates, CleanCandidate{
			Kind:         "worktree",
			ID:           id,
			Path:         wt.Path,
			Branch:       wt.Branch,
			Reason:       fmt.Sprintf("worktree has no refs and is older than %.1f hours", hours),
			LastActivity: lastActivity,
		})
	}

	for id := range refIDs {
		if _, ok := worktrees[id]; ok {
			continue
		}
		state, err := s.loadState(id)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, CleanCandidate{
			Kind:   "ref",
			ID:     id,
			Path:   state.pathOrEmpty(),
			Branch: state.branchOrDefault(id),
			Reason: "refs exist without a registered worktree",
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates, nil
}

func (s *Service) ApplyClean(candidates []CleanCandidate) (*CleanResult, error) {
	removed := make([]CleanCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.Kind {
		case "worktree":
			if err := s.unlockWorktree(candidate.Path); err != nil {
				return nil, err
			}
			if err := s.removeWorktree(candidate.Path); err != nil {
				return nil, err
			}
			if strings.HasPrefix(candidate.Branch, "agent/") {
				if err := s.deleteBranch(candidate.Branch); err != nil {
					return nil, err
				}
			}
		case "ref":
			if err := s.deleteAgentRefs(candidate.ID); err != nil {
				return nil, err
			}
			if strings.HasPrefix(candidate.Branch, "agent/") {
				if err := s.deleteBranch(candidate.Branch); err != nil {
					return nil, err
				}
			}
		default:
			return nil, fmt.Errorf("unknown clean candidate kind %q", candidate.Kind)
		}
		removed = append(removed, candidate)
	}
	return &CleanResult{Removed: removed}, nil
}

func (s *Service) RepoName() string {
	return filepath.Base(s.Repo)
}

func (s *Service) EffectiveConfig() Config {
	return s.Config
}

type AgentPreflight struct {
	ID              string   `json:"id"`
	Path            string   `json:"path,omitempty"`
	Branch          string   `json:"branch,omitempty"`
	Base            string   `json:"base,omitempty"`
	Latest          string   `json:"latest,omitempty"`
	Locked          bool     `json:"locked"`
	ShouldStop      bool     `json:"should_stop"`
	StopReason      string   `json:"stop_reason,omitempty"`
	CurrentChanges  int      `json:"current_changes"`
	CurrentPaths    []string `json:"current_paths,omitempty"`
	SnapshotCount   int      `json:"snapshot_count"`
	DoneAuthorName  string   `json:"done_author_name,omitempty"`
	DoneAuthorEmail string   `json:"done_author_email,omitempty"`
	DoneMessage     string   `json:"done_message_preview,omitempty"`
	SnapshotMessage string   `json:"snapshot_message_preview,omitempty"`
	DefaultOwner    string   `json:"default_owner,omitempty"`
	RefreshSeconds  int      `json:"refresh_seconds"`
	CleanHours      float64  `json:"clean_threshold_hours"`
}

func (s *Service) AgentPreflightInfo(id string) (*AgentPreflight, error) {
	status, err := s.Status(id)
	if err != nil {
		return nil, err
	}

	currentPaths := make([]string, 0, len(status.CurrentChanges))
	for _, change := range status.CurrentChanges {
		currentPaths = append(currentPaths, change.Path)
	}

	stopReason := ""
	shouldStop := false
	if status.Stop != nil {
		shouldStop = true
		stopReason = status.Stop.Reason
	}

	return &AgentPreflight{
		ID:              id,
		Path:            status.Summary.Path,
		Branch:          status.Summary.Branch,
		Base:            status.Base,
		Latest:          status.Latest,
		Locked:          status.Locked,
		ShouldStop:      shouldStop,
		StopReason:      stopReason,
		CurrentChanges:  len(status.CurrentChanges),
		CurrentPaths:    currentPaths,
		SnapshotCount:   len(status.Snapshots),
		DoneAuthorName:  firstNonEmpty(s.Config.DoneAuthorName, status.Summary.Owner),
		DoneAuthorEmail: s.Config.DoneAuthorEmail,
		DoneMessage:     s.defaultDoneMessage(id, statusToMeta(status)),
		SnapshotMessage: s.renderTemplate(s.Config.SnapshotMessageFormat, id, statusToMeta(status), time.Now().UTC()),
		DefaultOwner:    s.Config.DefaultOwner,
		RefreshSeconds:  s.Config.DashboardRefreshSecs,
		CleanHours:      s.Config.CleanThresholdHours,
	}, nil
}

func (s *Service) loadState(id string) (*agentState, error) {
	worktrees, err := s.listWorktrees()
	if err != nil {
		return nil, err
	}
	meta, err := s.readMeta(id)
	if err != nil {
		return nil, err
	}
	base, _, err := s.readRef(s.baseRef(id))
	if err != nil {
		return nil, err
	}
	latest, _, err := s.readRef(s.latestRef(id))
	if err != nil {
		return nil, err
	}
	stop, err := s.readStop(id)
	if err != nil {
		return nil, err
	}

	state := &agentState{
		ID:       id,
		Worktree: worktrees[id],
		Meta:     meta,
		Base:     base,
		Latest:   latest,
		Stop:     stop,
	}
	if state.Worktree == nil && state.Meta == nil && state.Base == "" && state.Latest == "" && state.Stop == nil {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	return state, nil
}

func (s *Service) agentIDs() (map[string]struct{}, error) {
	out, err := s.git("", nil, "", "for-each-ref", "--format=%(refname)", "refs/agents")
	if err != nil {
		return nil, err
	}
	ids := map[string]struct{}{}
	if strings.TrimSpace(out) == "" {
		return ids, nil
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" {
			continue
		}
		trimmed := strings.TrimPrefix(ref, "refs/agents/")
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			ids[parts[0]] = struct{}{}
		}
	}
	return ids, nil
}

func (s *Service) listWorktrees() (map[string]*WorktreeInfo, error) {
	out, err := s.git("", nil, "", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	result := map[string]*WorktreeInfo{}
	var current *WorktreeInfo
	flush := func() {
		if current == nil {
			return
		}
		id := filepath.Base(current.Path)
		if current.Main {
			current = nil
			return
		}
		result[id] = current
		current = nil
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			path := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			current = &WorktreeInfo{
				Path: path,
				Main: filepath.Clean(path) == filepath.Clean(s.Repo),
			}
		case strings.HasPrefix(line, "HEAD ") && current != nil:
			current.Head = strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
		case strings.HasPrefix(line, "branch ") && current != nil:
			branch := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			current.Branch = strings.TrimPrefix(branch, "refs/heads/")
		case strings.HasPrefix(line, "locked") && current != nil:
			current.Locked = true
			current.LockedReason = strings.TrimSpace(strings.TrimPrefix(line, "locked"))
		}
	}
	flush()
	return result, nil
}

func (s *Service) readMeta(id string) (*AgentMeta, error) {
	sha, ok, err := s.readRef(s.metaRef(id))
	if err != nil || !ok {
		return nil, err
	}
	out, err := s.git("", nil, "", "cat-file", "-p", sha)
	if err != nil {
		return nil, err
	}
	var meta AgentMeta
	if err := json.Unmarshal([]byte(out), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Service) readStop(id string) (*StopSignal, error) {
	sha, ok, err := s.readRef(s.stopRef(id))
	if err != nil || !ok {
		return nil, err
	}
	out, err := s.git("", nil, "", "cat-file", "-p", sha)
	if err != nil {
		return nil, err
	}
	var stop StopSignal
	if err := json.Unmarshal([]byte(out), &stop); err != nil {
		return nil, err
	}
	return &stop, nil
}

func (s *Service) readRef(ref string) (string, bool, error) {
	out, err := s.git("", nil, "", "for-each-ref", "--format=%(objectname)", ref)
	if err != nil {
		return "", false, err
	}
	value := strings.TrimSpace(out)
	if value == "" {
		return "", false, nil
	}
	return strings.Split(value, "\n")[0], true, nil
}

func (s *Service) writeJSONRef(ref string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	sha, err := s.git("", nil, string(payload), "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	return s.updateRef(ref, sha)
}

func (s *Service) updateRef(ref, value string) error {
	_, err := s.git("", nil, "", "update-ref", ref, value)
	return err
}

func (s *Service) deleteAgentRefs(id string) error {
	out, err := s.git("", nil, "", "for-each-ref", "--format=%(refname)", "refs/agents/"+id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" {
			continue
		}
		if _, err := s.git("", nil, "", "update-ref", "-d", ref); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deleteRefIfExists(ref string) error {
	_, ok, err := s.readRef(ref)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	_, err = s.git("", nil, "", "update-ref", "-d", ref)
	return err
}

func (s *Service) lockWorktree(path, reason string) error {
	args := []string{"worktree", "lock"}
	if strings.TrimSpace(reason) != "" {
		args = append(args, "--reason", reason)
	}
	args = append(args, path)
	_, err := s.git("", nil, "", args...)
	if err != nil && strings.Contains(err.Error(), "already locked") {
		return nil
	}
	return err
}

func (s *Service) unlockWorktree(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	_, err := s.git("", nil, "", "worktree", "unlock", path)
	if err != nil && strings.Contains(err.Error(), "is not locked") {
		return nil
	}
	if err != nil && strings.Contains(err.Error(), "does not exist") {
		return nil
	}
	return err
}

func (s *Service) removeWorktree(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	_, err := s.git("", nil, "", "worktree", "remove", "--force", path)
	return err
}

func (s *Service) deleteBranch(branch string) error {
	if strings.TrimSpace(branch) == "" {
		return nil
	}
	out, err := s.git("", nil, "", "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	_, err = s.git("", nil, "", "branch", "-D", branch)
	return err
}

func (s *Service) snapshotCount(state *agentState) (int, error) {
	if state.Base == "" || state.Latest == "" {
		return 0, nil
	}
	out, err := s.git("", nil, "", "rev-list", "--count", "--first-parent", state.Latest, "^"+state.Base)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

func (s *Service) diffStat(state *agentState) (DiffStat, error) {
	if state.Base == "" {
		return DiffStat{}, nil
	}

	var out string
	var err error
	if state.Worktree != nil {
		out, err = s.git(state.Worktree.Path, nil, "", "diff", "--shortstat", state.Base, "--")
	} else if state.Latest != "" {
		out, err = s.git("", nil, "", "diff", "--shortstat", state.Base, state.Latest, "--")
	} else {
		return DiffStat{}, nil
	}
	if err != nil {
		return DiffStat{}, err
	}
	return parseDiffStat(out), nil
}

func (s *Service) lastActivity(state *agentState) (string, error) {
	var candidates []time.Time

	if state.Meta != nil {
		if t, ok := parseTime(state.Meta.CreatedAt); ok {
			candidates = append(candidates, t)
		}
	}
	if state.Stop != nil {
		if t, ok := parseTime(state.Stop.CreatedAt); ok {
			candidates = append(candidates, t)
		}
	}
	if state.Latest != "" {
		out, err := s.git("", nil, "", "show", "-s", "--format=%cI", state.Latest)
		if err != nil {
			return "", err
		}
		if t, ok := parseTime(strings.TrimSpace(out)); ok {
			candidates = append(candidates, t)
		}
	}
	if state.Worktree != nil {
		modTime, err := latestFileModTime(state.Worktree.Path)
		if err != nil {
			return "", err
		}
		if !modTime.IsZero() {
			candidates = append(candidates, modTime)
		}
	}

	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	return candidates[len(candidates)-1].UTC().Format(time.RFC3339), nil
}

func (s *Service) snapshots(state *agentState) ([]SnapshotInfo, error) {
	if state.Base == "" || state.Latest == "" {
		return nil, nil
	}
	out, err := s.git("", nil, "", "rev-list", "--first-parent", state.Latest, "^"+state.Base)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	commits := strings.Split(strings.TrimSpace(out), "\n")
	snapshots := make([]SnapshotInfo, 0, len(commits))
	total := len(commits)
	for i, commit := range commits {
		name := fmt.Sprintf("snap-%d", total-i)
		snapshot, err := s.snapshotInfo(commit, name)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, *snapshot)
	}
	return snapshots, nil
}

func (s *Service) currentChanges(state *agentState) ([]FileChange, error) {
	if state.Worktree == nil {
		return nil, nil
	}

	baseline := state.Latest
	if baseline == "" {
		baseline = state.Base
	}
	if baseline == "" {
		out, err := s.git(state.Worktree.Path, nil, "", "status", "--porcelain=v1", "--untracked-files=normal")
		if err != nil {
			return nil, err
		}
		return parsePorcelainChanges(out), nil
	}

	tmpIndex, err := os.CreateTemp("", fmt.Sprintf("agt-%s-status-*.idx", state.ID))
	if err != nil {
		return nil, err
	}
	tmpIndexPath := tmpIndex.Name()
	if err := tmpIndex.Close(); err != nil {
		return nil, err
	}
	defer os.Remove(tmpIndexPath)

	env := []string{"GIT_INDEX_FILE=" + tmpIndexPath}
	if _, err := s.git(state.Worktree.Path, env, "", "read-tree", baseline); err != nil {
		return nil, err
	}
	if _, err := s.git(state.Worktree.Path, env, "", "add", "-A", "--", "."); err != nil {
		return nil, err
	}
	out, err := s.git(state.Worktree.Path, env, "", "diff", "--cached", "--name-status", "--find-renames", baseline, "--")
	if err != nil {
		return nil, err
	}
	return parseNameStatusChanges(out), nil
}

func (s *Service) snapshotInfo(commit, name string) (*SnapshotInfo, error) {
	out, err := s.git("", nil, "", "show", "-s", "--format=%H%x00%P%x00%cI%x00%s", commit)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, "\x00")
	if len(parts) < 4 {
		return nil, fmt.Errorf("unexpected commit metadata format for %s", commit)
	}
	parent := ""
	parentParts := strings.Fields(parts[1])
	if len(parentParts) > 0 {
		parent = parentParts[0]
	}

	diffOut, err := s.git("", nil, "", "diff-tree", "--no-commit-id", "--name-status", "-r", commit)
	if err != nil {
		return nil, err
	}
	changes := parseNameStatusChanges(diffOut)

	return &SnapshotInfo{
		Name:      name,
		Commit:    parts[0],
		Parent:    parent,
		Timestamp: parts[2],
		Message:   parts[3],
		Changes:   changes,
	}, nil
}

func (s *Service) CommitFileDiff(commit, parent, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	resolvedPath := normalizePathForContent(path)
	if strings.TrimSpace(parent) == "" {
		return s.git("", nil, "", "show", commit, "--", resolvedPath)
	}
	return s.git("", nil, "", "diff", parent, commit, "--", resolvedPath)
}

func (s *Service) CommitFileContent(commit, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	resolvedPath := normalizePathForContent(path)
	return s.git("", nil, "", "show", fmt.Sprintf("%s:%s", commit, resolvedPath))
}

func (s *Service) resolveSnapshotSpec(id, spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "latest" {
		ref, ok, err := s.readRef(s.latestRef(id))
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("agent %q has no snapshots", id)
		}
		return ref, nil
	}
	if spec == "base" {
		ref, ok, err := s.readRef(s.baseRef(id))
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("agent %q has no base ref", id)
		}
		return ref, nil
	}
	if strings.HasPrefix(spec, "~") {
		refName := s.latestRef(id) + spec
		return s.git("", nil, "", "rev-parse", refName)
	}
	if strings.HasPrefix(spec, "snap-") {
		index, err := strconv.Atoi(strings.TrimPrefix(spec, "snap-"))
		if err != nil || index < 1 {
			return "", fmt.Errorf("invalid snapshot spec %q", spec)
		}
		state, err := s.loadState(id)
		if err != nil {
			return "", err
		}
		count, err := s.snapshotCount(state)
		if err != nil {
			return "", err
		}
		if index > count {
			return "", fmt.Errorf("snapshot %q does not exist", spec)
		}
		offset := count - index
		if offset == 0 {
			ref, ok, err := s.readRef(s.latestRef(id))
			if err != nil {
				return "", err
			}
			if !ok {
				return "", fmt.Errorf("agent %q has no latest snapshot", id)
			}
			return ref, nil
		}
		refName := fmt.Sprintf("%s~%d", s.latestRef(id), offset)
		return s.git("", nil, "", "rev-parse", refName)
	}
	return s.git("", nil, "", "rev-parse", spec+"^{commit}")
}

func (s *Service) resolveDiffSide(id, spec string) (string, bool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", true, nil
	}
	if spec == "current" {
		return "", true, nil
	}
	commit, err := s.resolveSnapshotSpec(id, spec)
	if err == nil {
		return commit, false, nil
	}
	return "", false, err
}

func (s *Service) authorEnv(name, email string) []string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = "agent"
	}
	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail == "" {
		trimmedEmail = "agent@local"
	}
	return []string{
		"GIT_AUTHOR_NAME=" + trimmedName,
		"GIT_AUTHOR_EMAIL=" + trimmedEmail,
		"GIT_COMMITTER_NAME=" + trimmedName,
		"GIT_COMMITTER_EMAIL=" + trimmedEmail,
	}
}

func (s *Service) authorName(opts DoneOptions, meta *AgentMeta) string {
	if strings.TrimSpace(opts.AuthorName) != "" {
		return opts.AuthorName
	}
	if strings.TrimSpace(s.Config.DoneAuthorName) != "" {
		return s.Config.DoneAuthorName
	}
	if meta != nil && strings.TrimSpace(meta.Owner) != "" {
		return meta.Owner
	}
	return "agent"
}

func (s *Service) authorEmail(opts DoneOptions) string {
	if strings.TrimSpace(opts.AuthorEmail) != "" {
		return opts.AuthorEmail
	}
	if strings.TrimSpace(s.Config.DoneAuthorEmail) != "" {
		return s.Config.DoneAuthorEmail
	}
	return ""
}

func (s *Service) defaultDoneMessage(id string, meta *AgentMeta) string {
	message := s.renderTemplate(s.Config.DoneMessageTemplate, id, meta, time.Now().UTC())
	if strings.TrimSpace(message) != "" {
		return message
	}
	return fmt.Sprintf("agent(%s): finalize work", id)
}

func (s *Service) metaRef(id string) string   { return "refs/agents/" + id + "/meta" }
func (s *Service) baseRef(id string) string   { return "refs/agents/" + id + "/base" }
func (s *Service) latestRef(id string) string { return "refs/agents/" + id + "/latest" }
func (s *Service) stopRef(id string) string   { return "refs/agents/" + id + "/stop" }

func (s *Service) buildTreeFromWorktree(id, worktreePath, parent string) (string, error) {
	tmpIndex, err := os.CreateTemp("", fmt.Sprintf("agt-%s-*.idx", id))
	if err != nil {
		return "", err
	}
	tmpIndexPath := tmpIndex.Name()
	if err := tmpIndex.Close(); err != nil {
		return "", err
	}
	defer os.Remove(tmpIndexPath)

	env := []string{"GIT_INDEX_FILE=" + tmpIndexPath}
	if strings.TrimSpace(parent) != "" {
		if _, err := s.git(worktreePath, env, "", "read-tree", parent); err != nil {
			return "", err
		}
	}
	if _, err := s.git(worktreePath, env, "", "add", "-A", "--", "."); err != nil {
		return "", err
	}
	return s.git(worktreePath, env, "", "write-tree")
}

func (s *Service) treeForCommit(commit string) (string, error) {
	return s.git("", nil, "", "show", "-s", "--format=%T", commit)
}

func (s *Service) createCommit(tree, parent, message string, env []string) (string, error) {
	args := []string{"commit-tree", tree}
	if strings.TrimSpace(parent) != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)
	return s.git("", env, "", args...)
}

func (s *Service) git(dir string, env []string, stdin string, args ...string) (string, error) {
	fullArgs := make([]string, 0, len(args)+2)
	if dir != "" {
		fullArgs = append(fullArgs, "-C", dir)
	}
	fullArgs = append(fullArgs, args...)

	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = s.Repo
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(fullArgs, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func parseDiffStat(out string) DiffStat {
	stat := DiffStat{}
	if match := filesChangedPattern.FindStringSubmatch(out); len(match) == 2 {
		stat.Files, _ = strconv.Atoi(match[1])
	}
	if match := insertionsPattern.FindStringSubmatch(out); len(match) == 2 {
		stat.Insertions, _ = strconv.Atoi(match[1])
	}
	if match := deletionsPattern.FindStringSubmatch(out); len(match) == 2 {
		stat.Deletions, _ = strconv.Atoi(match[1])
	}
	return stat
}

func parsePorcelainChanges(out string) []FileChange {
	if strings.TrimSpace(out) == "" {
		return nil
	}

	changes := make([]FileChange, 0)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		status := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = strings.TrimSpace(parts[len(parts)-1])
		}
		if status == "" {
			status = "??"
		}
		changes = append(changes, FileChange{
			Path:   path,
			Status: status,
		})
	}
	return changes
}

func parseNameStatusChanges(out string) []FileChange {
	if strings.TrimSpace(out) == "" {
		return nil
	}

	changes := make([]FileChange, 0)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := strings.TrimSpace(fields[0])
		path := fields[len(fields)-1]
		if len(fields) > 2 && strings.HasPrefix(status, "R") {
			path = strings.TrimSpace(fields[1]) + " -> " + strings.TrimSpace(fields[2])
		}
		changes = append(changes, FileChange{
			Path:   strings.TrimSpace(path),
			Status: status,
		})
	}
	return changes
}

func latestFileModTime(root string) (time.Time, error) {
	var latest time.Time
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return latest, err
}

func parseTime(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func statusWeight(status string) int {
	switch status {
	case "active":
		return 0
	case "stopped":
		return 1
	case "orphaned":
		return 2
	default:
		return 3
	}
}

func (s *agentState) status() string {
	switch {
	case s.Stop != nil:
		return "stopped"
	case s.Worktree != nil:
		return "active"
	case s.Meta != nil || s.Base != "" || s.Latest != "":
		return "orphaned"
	default:
		return "unknown"
	}
}

func (s *agentState) pathOrEmpty() string {
	if s.Worktree != nil {
		return s.Worktree.Path
	}
	if s.Meta != nil {
		return s.Meta.Path
	}
	return ""
}

func (s *agentState) branchOrDefault(id string) string {
	if s.Worktree != nil && s.Worktree.Branch != "" {
		return s.Worktree.Branch
	}
	if s.Meta != nil && s.Meta.Branch != "" {
		return s.Meta.Branch
	}
	return "agent/" + id
}

func (s *agentState) purposeOrEmpty() string {
	if s.Meta == nil {
		return ""
	}
	return s.Meta.Purpose
}

func (s *agentState) metaOwnerOrDefault() string {
	if s.Meta != nil && strings.TrimSpace(s.Meta.Owner) != "" {
		return s.Meta.Owner
	}
	return ""
}

func (s *agentState) worktreeLocked() bool {
	return s.Worktree != nil && s.Worktree.Locked
}

func normalizePathForContent(path string) string {
	trimmed := strings.TrimSpace(path)
	if strings.Contains(trimmed, " -> ") {
		parts := strings.Split(trimmed, " -> ")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	return trimmed
}

func (s *Service) renderTemplate(template, id string, meta *AgentMeta, ts time.Time) string {
	if strings.TrimSpace(template) == "" {
		return ""
	}
	purpose := ""
	owner := ""
	branch := "agent/" + id
	if meta != nil {
		purpose = meta.Purpose
		owner = meta.Owner
		if meta.Branch != "" {
			branch = meta.Branch
		}
	}
	purpose = firstNonEmpty(purpose, "finalize work")
	rendered := strings.NewReplacer(
		"{id}", id,
		"{purpose}", purpose,
		"{owner}", owner,
		"{branch}", branch,
		"{timestamp}", ts.Format(time.RFC3339),
	).Replace(template)
	return strings.TrimSpace(rendered)
}

func statusToMeta(status *AgentStatus) *AgentMeta {
	if status == nil {
		return nil
	}
	return &AgentMeta{
		ID:      status.Summary.ID,
		Purpose: status.Summary.Purpose,
		Owner:   status.Summary.Owner,
		Branch:  status.Summary.Branch,
		Path:    status.Summary.Path,
	}
}
