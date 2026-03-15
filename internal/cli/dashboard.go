package cli

import (
	"agt/internal/app"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type refreshMsg struct{}

type dashMode string

const (
	modeList    dashMode = "list"
	modeDetail  dashMode = "detail"
	modeDiff    dashMode = "diff"
	modeContent dashMode = "content"
)

type dashFocus string

const (
	focusSnapshots dashFocus = "snapshots"
	focusFiles     dashFocus = "files"
)

type dashModel struct {
	svc           *app.Service
	mode          dashMode
	focus         dashFocus
	entries       []app.AgentSummary
	selected      int
	detail        *app.AgentStatus
	snapshotIndex int
	fileIndex     int
	fileBody      string
	fileTitle     string
	statusLine    string
	err           error
}

func runDashboard(svc *app.Service) error {
	entries, err := svc.ListAgents()
	if err != nil {
		return err
	}
	model := dashModel{
		svc:     svc,
		mode:    modeList,
		focus:   focusSnapshots,
		entries: entries,
	}
	model.statusLine = "[j/k] move  [enter] detail  [r] refresh  [q] quit"
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

func (m dashModel) Init() tea.Cmd {
	return tickDashboard()
}

func (m dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m.refresh()
		case "esc", "backspace":
			return m.goBack()
		}

		switch m.mode {
		case modeList:
			return m.updateList(msg.String())
		case modeDetail:
			return m.updateDetail(msg.String())
		case modeDiff, modeContent:
			return m.updateFileView(msg.String())
		}
	case refreshMsg:
		next, _ := m.refresh()
		return next, tickDashboard()
	}
	return m, nil
}

func (m dashModel) View() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("repo: %s\n\n", m.svc.RepoName()))
	if m.err != nil {
		b.WriteString("error: " + m.err.Error() + "\n\n")
	}

	switch m.mode {
	case modeList:
		b.WriteString(m.renderList())
	case modeDetail:
		b.WriteString(m.renderDetail())
	case modeDiff, modeContent:
		b.WriteString(m.renderFileView())
	}

	if m.statusLine != "" {
		b.WriteString("\n" + m.statusLine + "\n")
	}
	return b.String()
}

func (m dashModel) updateList(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		if m.selected < len(m.entries)-1 {
			m.selected++
		}
	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}
	case "enter":
		if len(m.entries) == 0 {
			return m, nil
		}
		if err := m.loadDetail(m.entries[m.selected].ID); err != nil {
			m.err = err
			return m, nil
		}
	}
	return m, nil
}

func (m dashModel) updateDetail(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab":
		if m.focus == focusSnapshots {
			m.focus = focusFiles
		} else {
			m.focus = focusSnapshots
		}
	case "j", "down":
		if m.focus == focusSnapshots {
			if m.snapshotIndex < len(m.detail.Snapshots)-1 {
				m.snapshotIndex++
				m.fileIndex = 0
			}
		} else if m.fileIndex < len(m.currentChanges())-1 {
			m.fileIndex++
		}
	case "k", "up":
		if m.focus == focusSnapshots {
			if m.snapshotIndex > 0 {
				m.snapshotIndex--
				m.fileIndex = 0
			}
		} else if m.fileIndex > 0 {
			m.fileIndex--
		}
	case "d":
		if err := m.openFileView(modeDiff); err != nil {
			m.err = err
		}
	case "f":
		if err := m.openFileView(modeContent); err != nil {
			m.err = err
		}
	}
	return m, nil
}

func (m dashModel) updateFileView(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "d":
		if err := m.openFileView(modeDiff); err != nil {
			m.err = err
		}
	case "f":
		if err := m.openFileView(modeContent); err != nil {
			m.err = err
		}
	}
	return m, nil
}

func (m dashModel) refresh() (dashModel, tea.Cmd) {
	entries, err := m.svc.ListAgents()
	m.entries = entries
	m.err = err
	if m.selected >= len(m.entries) && len(m.entries) > 0 {
		m.selected = len(m.entries) - 1
	}
	if len(m.entries) == 0 {
		m.selected = 0
	}

	if m.mode == modeDetail && m.detail != nil {
		if err := m.loadDetail(m.detail.Summary.ID); err != nil {
			m.err = err
			m.mode = modeList
			m.detail = nil
			m.statusLine = "[j/k] move  [enter] detail  [r] refresh  [q] quit"
		}
	}
	if m.mode == modeList {
		m.statusLine = "[j/k] move  [enter] detail  [r] refresh  [q] quit"
	}
	return m, tickDashboard()
}

func (m dashModel) goBack() (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeDiff, modeContent:
		m.mode = modeDetail
		m.fileBody = ""
		m.fileTitle = ""
		if m.detail != nil {
			m.statusLine = fmt.Sprintf("detail: %s  [tab] focus  [d] diff  [f] file  [esc] back", m.detail.Summary.ID)
		}
	case modeDetail:
		m.mode = modeList
		m.detail = nil
		m.snapshotIndex = 0
		m.fileIndex = 0
		m.focus = focusSnapshots
		m.statusLine = "[j/k] move  [enter] detail  [r] refresh  [q] quit"
	}
	return m, nil
}

func (m *dashModel) loadDetail(id string) error {
	status, err := m.svc.Status(id)
	if err != nil {
		return err
	}
	m.detail = status
	m.mode = modeDetail
	m.focus = focusSnapshots
	if m.snapshotIndex >= len(status.Snapshots) {
		m.snapshotIndex = 0
	}
	if m.fileIndex >= len(m.currentChanges()) {
		m.fileIndex = 0
	}
	m.statusLine = fmt.Sprintf("detail: %s  [tab] focus  [d] diff  [f] file  [esc] back", id)
	return nil
}

func (m *dashModel) openFileView(mode dashMode) error {
	snapshot := m.currentSnapshot()
	if snapshot == nil {
		return fmt.Errorf("no snapshot selected")
	}
	changes := m.currentChanges()
	if len(changes) == 0 {
		return fmt.Errorf("selected snapshot has no file changes")
	}
	if m.fileIndex >= len(changes) {
		m.fileIndex = 0
	}
	change := changes[m.fileIndex]

	var body string
	var err error
	switch mode {
	case modeDiff:
		body, err = m.svc.CommitFileDiff(snapshot.Commit, snapshot.Parent, change.Path)
	case modeContent:
		if strings.HasPrefix(change.Status, "D") {
			body = "<deleted in this snapshot>"
		} else {
			body, err = m.svc.CommitFileContent(snapshot.Commit, change.Path)
		}
	default:
		return fmt.Errorf("unknown file view mode %q", mode)
	}
	if err != nil {
		return err
	}

	m.mode = mode
	m.fileBody = body
	m.fileTitle = fmt.Sprintf("%s  %s  %s", snapshot.Name, change.Status, change.Path)
	m.statusLine = "[d] diff  [f] file  [esc] back  [q] quit"
	return nil
}

func (m dashModel) renderList() string {
	var b strings.Builder
	if len(m.entries) == 0 {
		b.WriteString("no agent worktrees\n\n")
		b.WriteString("[q] quit  [r] refresh\n")
		return b.String()
	}

	writeSection := func(title, status string) {
		b.WriteString(title + "\n")
		count := 0
		for i, entry := range m.entries {
			if entry.Status != status {
				continue
			}
			count++
			cursor := " "
			if i == m.selected {
				cursor = ">"
			}
			owner := entry.Owner
			if owner == "" {
				owner = "-"
			}
			activity := entry.LastActivity
			if activity == "" {
				activity = "-"
			}
			b.WriteString(fmt.Sprintf("%s %-14s %-10s snaps=%-3d +%d -%d %s\n",
				cursor,
				entry.ID,
				owner,
				entry.Snapshots,
				entry.DiffStat.Insertions,
				entry.DiffStat.Deletions,
				activity,
			))
		}
		if count == 0 {
			b.WriteString("  (none)\n")
		}
		b.WriteString("\n")
	}

	writeSection("ACTIVE", "active")
	writeSection("STOPPED", "stopped")
	writeSection("ORPHANED", "orphaned")
	b.WriteString("[j/k] move  [enter] detail  [r] refresh  [q] quit\n")
	return b.String()
}

func (m dashModel) renderDetail() string {
	var b strings.Builder
	if m.detail == nil {
		return "no detail loaded\n"
	}

	summary := m.detail.Summary
	b.WriteString(fmt.Sprintf("%s  %s  snaps=%d  locked=%t\n", summary.ID, summary.Status, summary.Snapshots, m.detail.Locked))
	if summary.Owner != "" {
		b.WriteString(fmt.Sprintf("owner: %s\n", summary.Owner))
	}
	if summary.Purpose != "" {
		b.WriteString(fmt.Sprintf("purpose: %s\n", summary.Purpose))
	}
	b.WriteString(fmt.Sprintf("path: %s\n", summary.Path))
	b.WriteString(fmt.Sprintf("branch: %s\n\n", summary.Branch))
	if len(m.detail.CurrentChanges) > 0 {
		b.WriteString("Current Changes\n")
		for _, change := range m.detail.CurrentChanges {
			b.WriteString(fmt.Sprintf("  %-4s %s\n", change.Status, change.Path))
		}
		b.WriteString("\n")
	}

	b.WriteString(m.renderSnapshotPanel())
	b.WriteString("\n")
	b.WriteString(m.renderFilePanel())
	return b.String()
}

func (m dashModel) renderSnapshotPanel() string {
	var b strings.Builder
	label := "Snapshots"
	if m.focus == focusSnapshots {
		label += " [focus]"
	}
	b.WriteString(label + "\n")
	if m.detail == nil || len(m.detail.Snapshots) == 0 {
		b.WriteString("  (none)\n")
		return b.String()
	}
	for i, snapshot := range m.detail.Snapshots {
		cursor := " "
		if i == m.snapshotIndex {
			cursor = ">"
		}
		b.WriteString(fmt.Sprintf("%s %-8s %s %s\n", cursor, snapshot.Name, snapshot.Timestamp, snapshot.Commit))
	}
	return b.String()
}

func (m dashModel) renderFilePanel() string {
	var b strings.Builder
	label := "Files"
	if m.focus == focusFiles {
		label += " [focus]"
	}
	b.WriteString(label + "\n")
	changes := m.currentChanges()
	if len(changes) == 0 {
		b.WriteString("  (none)\n")
		return b.String()
	}
	for i, change := range changes {
		cursor := " "
		if i == m.fileIndex {
			cursor = ">"
		}
		b.WriteString(fmt.Sprintf("%s %-4s %s\n", cursor, change.Status, change.Path))
	}
	return b.String()
}

func (m dashModel) renderFileView() string {
	var b strings.Builder
	title := "file view"
	if m.mode == modeDiff {
		title = "diff view"
	}
	b.WriteString(title + "\n")
	b.WriteString(m.fileTitle + "\n")
	b.WriteString(strings.Repeat("-", max(20, len(m.fileTitle))) + "\n")
	if strings.TrimSpace(m.fileBody) == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(m.fileBody + "\n")
	}
	return b.String()
}

func (m dashModel) currentSnapshot() *app.SnapshotInfo {
	if m.detail == nil || len(m.detail.Snapshots) == 0 {
		return nil
	}
	if m.snapshotIndex < 0 || m.snapshotIndex >= len(m.detail.Snapshots) {
		return nil
	}
	return &m.detail.Snapshots[m.snapshotIndex]
}

func (m dashModel) currentChanges() []app.FileChange {
	snapshot := m.currentSnapshot()
	if snapshot == nil {
		return nil
	}
	return snapshot.Changes
}

func tickDashboard() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return refreshMsg{}
	})
}
