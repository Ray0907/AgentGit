package cli

import (
	"agt/internal/app"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
type dashAction string

const (
	focusSnapshots dashFocus = "snapshots"
	focusFiles     dashFocus = "files"
)

const (
	actionNone     dashAction = ""
	actionStop     dashAction = "stop"
	actionResume   dashAction = "resume"
	actionRollback dashAction = "rollback"
	actionAbort    dashAction = "abort"
	actionDone     dashAction = "done"
)

var (
	dashBorder      = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8"))
	dashPanel       = dashBorder.Padding(0, 1)
	dashPanelFocus  = dashBorder.BorderForeground(lipgloss.Color("12")).Padding(0, 1)
	dashHeader      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dashMuted       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	dashAccent      = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	dashWarn        = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	dashStop        = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	dashSelected    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10")).Bold(true)
	dashHelp        = dashBorder.Padding(0, 1).Foreground(lipgloss.Color("7"))
	dashHeaderPanel = dashBorder.Padding(0, 1).Bold(true).Foreground(lipgloss.Color("15"))
)

const dashDefaultRefreshSeconds = 2

type dashModel struct {
	svc           *app.Service
	mode          dashMode
	focus         dashFocus
	entries       []app.AgentSummary
	selected      int
	preview       *app.AgentStatus
	detail        *app.AgentStatus
	snapshotIndex int
	fileIndex     int
	fileBody      string
	fileTitle     string
	statusLine    string
	confirmAction dashAction
	confirmPrompt string
	width         int
	height        int
	err           error
}

func runDashboard(svc *app.Service) error {
	entries, err := svc.ListAgents()
	if err != nil {
		return err
	}
	model := dashModel{
		svc:        svc,
		mode:       modeList,
		focus:      focusSnapshots,
		entries:    entries,
		width:      120,
		height:     36,
		statusLine: listStatusLine(),
	}
	if len(entries) > 0 {
		_ = model.loadPreview(entries[0].ID)
	}
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

func (m dashModel) Init() tea.Cmd {
	return tickDashboard(m.svc.Config.DashboardRefreshSecs)
}

func (m dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
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
		return next, tickDashboard(m.svc.Config.DashboardRefreshSecs)
	}
	return m, nil
}

func (m dashModel) View() string {
	switch m.mode {
	case modeList:
		return m.renderListScreen()
	case modeDetail:
		return m.renderDetailScreen()
	case modeDiff, modeContent:
		return m.renderFileScreen()
	default:
		return "unknown mode"
	}
}

func (m dashModel) updateList(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "r":
		return m.refresh()
	case "j", "down":
		if m.selected < len(m.entries)-1 {
			m.selected++
			_ = m.loadSelectedPreview()
		}
	case "k", "up":
		if m.selected > 0 {
			m.selected--
			_ = m.loadSelectedPreview()
		}
	case "enter":
		if len(m.entries) == 0 {
			return m, nil
		}
		if err := m.loadDetail(m.entries[m.selected].ID); err != nil {
			m.err = err
		}
	case "right":
		if len(m.entries) == 0 {
			return m, nil
		}
		if err := m.loadDetail(m.entries[m.selected].ID); err != nil {
			m.err = err
		}
	}
	return m, nil
}

func (m dashModel) updateDetail(key string) (tea.Model, tea.Cmd) {
	if m.confirmAction != actionNone {
		return m.updateConfirm(key)
	}
	switch key {
	case "tab", "left", "right":
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
	case "s":
		if err := m.beginConfirm(actionStop); err != nil {
			m.err = err
		}
	case "u":
		if err := m.beginConfirm(actionResume); err != nil {
			m.err = err
		}
	case "r":
		if err := m.beginConfirm(actionRollback); err != nil {
			m.err = err
		}
	case "x":
		if err := m.beginConfirm(actionAbort); err != nil {
			m.err = err
		}
	case "D":
		if err := m.beginConfirm(actionDone); err != nil {
			m.err = err
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

func (m dashModel) updateConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "enter":
		if err := m.executeConfirm(); err != nil {
			m.err = err
		}
	case "n":
		m.clearConfirm()
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
	if err := m.reloadEntries(m.selectedID()); err != nil {
		m.err = err
	}

	switch m.mode {
	case modeList:
		if err := m.loadSelectedPreview(); err != nil {
			m.err = err
		}
		m.statusLine = listStatusLine()
	case modeDetail:
		if m.detail != nil {
			if err := m.loadDetail(m.detail.Summary.ID); err != nil {
				m.err = err
				m.mode = modeList
				m.detail = nil
				m.statusLine = listStatusLine()
			}
		}
	}
	return m, tickDashboard(m.svc.Config.DashboardRefreshSecs)
}

func (m dashModel) goBack() (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeDiff, modeContent:
		m.mode = modeDetail
		m.fileBody = ""
		m.fileTitle = ""
		m.setDetailStatusLine()
	case modeDetail:
		if m.confirmAction != actionNone {
			m.clearConfirm()
			return m, nil
		}
		m.mode = modeList
		m.detail = nil
		m.snapshotIndex = 0
		m.fileIndex = 0
		m.focus = focusSnapshots
		m.statusLine = listStatusLine()
	}
	return m, nil
}

func (m *dashModel) loadSelectedPreview() error {
	if len(m.entries) == 0 {
		m.preview = nil
		return nil
	}
	return m.loadPreview(m.entries[m.selected].ID)
}

func (m *dashModel) loadPreview(id string) error {
	status, err := m.svc.Status(id)
	if err != nil {
		return err
	}
	m.preview = status
	return nil
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
	m.clearConfirm()
	m.setDetailStatusLine()
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

func (m *dashModel) beginConfirm(action dashAction) error {
	if m.detail == nil {
		return fmt.Errorf("no detail loaded")
	}

	switch action {
	case actionStop:
		if m.detail.Summary.Status != "active" {
			return fmt.Errorf("stop is only available for active agents")
		}
		m.confirmPrompt = fmt.Sprintf("stop %s? [y/N]", m.detail.Summary.ID)
	case actionResume:
		if m.detail.Summary.Status != "stopped" {
			return fmt.Errorf("resume is only available for stopped agents")
		}
		m.confirmPrompt = fmt.Sprintf("resume %s? [y/N]", m.detail.Summary.ID)
	case actionRollback:
		snapshot := m.currentSnapshot()
		if snapshot == nil {
			return fmt.Errorf("select a snapshot to roll back to")
		}
		m.confirmPrompt = fmt.Sprintf("rollback %s to %s? [y/N]", m.detail.Summary.ID, snapshot.Name)
	case actionAbort:
		m.confirmPrompt = fmt.Sprintf("abort %s and delete branch/worktree? [y/N]", m.detail.Summary.ID)
	case actionDone:
		m.confirmPrompt = fmt.Sprintf("finalize %s and remove worktree? [y/N]", m.detail.Summary.ID)
	default:
		return fmt.Errorf("unknown action %q", action)
	}

	m.confirmAction = action
	m.statusLine = dashWarn.Render(m.confirmPrompt)
	return nil
}

func (m *dashModel) executeConfirm() error {
	if m.detail == nil {
		return fmt.Errorf("no detail loaded")
	}

	id := m.detail.Summary.ID
	selectedID := m.selectedID()
	if selectedID == "" {
		selectedID = id
	}

	switch m.confirmAction {
	case actionStop:
		if _, err := m.svc.Stop(id, ""); err != nil {
			return err
		}
		if err := m.loadDetail(id); err != nil {
			return err
		}
	case actionResume:
		if _, err := m.svc.Resume(id); err != nil {
			return err
		}
		if err := m.loadDetail(id); err != nil {
			return err
		}
	case actionRollback:
		snapshot := m.currentSnapshot()
		if snapshot == nil {
			return fmt.Errorf("select a snapshot to roll back to")
		}
		if _, err := m.svc.Rollback(id, snapshot.Name, "dashboard rollback"); err != nil {
			return err
		}
		if err := m.loadDetail(id); err != nil {
			return err
		}
	case actionAbort:
		if _, err := m.svc.Abort(id); err != nil {
			return err
		}
		m.mode = modeList
		m.detail = nil
		if err := m.reloadEntries(selectedID); err != nil {
			return err
		}
		if err := m.loadSelectedPreview(); err != nil {
			m.preview = nil
		}
	case actionDone:
		if _, err := m.svc.Done(id, app.DoneOptions{}); err != nil {
			return err
		}
		m.mode = modeList
		m.detail = nil
		if err := m.reloadEntries(selectedID); err != nil {
			return err
		}
		if err := m.loadSelectedPreview(); err != nil {
			m.preview = nil
		}
	default:
		return fmt.Errorf("unknown action %q", m.confirmAction)
	}

	m.clearConfirm()
	return nil
}

func (m *dashModel) clearConfirm() {
	m.confirmAction = actionNone
	m.confirmPrompt = ""
	m.setDetailStatusLine()
}

func (m *dashModel) setDetailStatusLine() {
	if m.detail == nil {
		m.statusLine = listStatusLine()
		return
	}
	if m.confirmAction != actionNone {
		m.statusLine = dashWarn.Render(m.confirmPrompt)
		return
	}
	m.statusLine = fmt.Sprintf("detail: %s  [←/→] pane  [d] diff  [f] file  [s] stop  [u] resume  [r] rollback  [D] done  [x] abort  [esc] back",
		m.detail.Summary.ID)
}

func (m *dashModel) reloadEntries(selectedID string) error {
	entries, err := m.svc.ListAgents()
	if err != nil {
		return err
	}
	m.entries = entries
	if len(entries) == 0 {
		m.selected = 0
		m.preview = nil
		return nil
	}
	if selectedID != "" {
		for i, entry := range entries {
			if entry.ID == selectedID {
				m.selected = i
				return nil
			}
		}
	}
	if m.selected >= len(entries) {
		m.selected = len(entries) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	return nil
}

func (m dashModel) selectedID() string {
	if len(m.entries) == 0 || m.selected < 0 || m.selected >= len(m.entries) {
		return ""
	}
	return m.entries[m.selected].ID
}

func (m dashModel) renderListScreen() string {
	header := m.renderHeader("overview")
	stats := m.renderStatsBar()

	leftWidth := max(36, m.width/2-2)
	rightWidth := max(38, m.width-leftWidth-3)
	bodyHeight := max(12, m.height-8)

	left := dashPanelFocus.Width(leftWidth).Height(bodyHeight).Render(m.renderWorktreeList(leftWidth - 4))
	right := dashPanel.Width(rightWidth).Height(bodyHeight).Render(m.renderPreview(rightWidth-4, bodyHeight-2))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := dashHelp.Width(max(20, m.width-2)).Render(m.statusLine)
	return lipgloss.JoinVertical(lipgloss.Left, header, stats, body, footer)
}

func (m dashModel) renderDetailScreen() string {
	header := m.renderHeader("detail")
	stats := dashHeaderPanel.Width(max(20, m.width-2)).Render(m.renderDetailHeader())

	leftWidth := max(28, m.width/4)
	centerWidth := max(28, m.width/4)
	rightWidth := max(36, m.width-leftWidth-centerWidth-4)
	bodyHeight := max(12, m.height-8)

	leftPanel := dashPanel
	centerPanel := dashPanel
	if m.focus == focusSnapshots {
		leftPanel = dashPanelFocus
	}
	if m.focus == focusFiles {
		centerPanel = dashPanelFocus
	}

	left := leftPanel.Width(leftWidth).Height(bodyHeight).Render(m.renderSnapshotsPane(leftWidth-4, bodyHeight-2))
	center := centerPanel.Width(centerWidth).Height(bodyHeight).Render(m.renderFilesPane(centerWidth-4, bodyHeight-2))
	right := dashPanel.Width(rightWidth).Height(bodyHeight).Render(m.renderInspectorPane(rightWidth-4, bodyHeight-2))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	footer := dashHelp.Width(max(20, m.width-2)).Render(m.statusLine)
	return lipgloss.JoinVertical(lipgloss.Left, header, stats, body, footer)
}

func (m dashModel) renderFileScreen() string {
	title := "file"
	if m.mode == modeDiff {
		title = "diff"
	}
	header := m.renderHeader(title)
	info := dashHeaderPanel.Width(max(20, m.width-2)).Render(m.fileTitle)
	bodyHeight := max(12, m.height-6)
	content := dashPanel.Width(max(20, m.width-2)).Height(bodyHeight).Render(fitText(m.fileBody, max(16, m.width-6), bodyHeight-2))
	footer := dashHelp.Width(max(20, m.width-2)).Render(m.statusLine)
	return lipgloss.JoinVertical(lipgloss.Left, header, info, content, footer)
}

func (m dashModel) renderHeader(section string) string {
	title := dashHeader.Render("AgentGit Dashboard")
	repo := dashMuted.Render("repo: " + m.svc.RepoName())
	mode := dashAccent.Render("view: " + section)
	summary := dashMuted.Render(fmt.Sprintf("agents: %d", len(m.entries)))
	if m.err != nil {
		mode = dashStop.Render("error: " + m.err.Error())
	}
	return dashHeaderPanel.Width(max(20, m.width-2)).Render(lipgloss.JoinHorizontal(lipgloss.Top, title, "   ", repo, "   ", mode, "   ", summary))
}

func (m dashModel) renderStatsBar() string {
	active := 0
	stopped := 0
	orphaned := 0
	for _, entry := range m.entries {
		switch entry.Status {
		case "active":
			active++
		case "stopped":
			stopped++
		case "orphaned":
			orphaned++
		}
	}
	cardWidth := max(18, (m.width-4)/3)
	activeCard := dashPanelFocus.Width(cardWidth).Render("ACTIVE\n" + dashAccent.Render(fmt.Sprintf("%d agents", active)))
	stoppedCard := dashPanel.Width(cardWidth).Render("STOPPED\n" + dashWarn.Render(fmt.Sprintf("%d agents", stopped)))
	orphanedCard := dashPanel.Width(cardWidth).Render("ORPHANED\n" + dashStop.Render(fmt.Sprintf("%d agents", orphaned)))
	return lipgloss.JoinHorizontal(lipgloss.Top, activeCard, stoppedCard, orphanedCard)
}

func (m dashModel) renderWorktreeList(width int) string {
	var lines []string
	lines = append(lines, dashHeader.Render(fmt.Sprintf("Worktrees (%d)", len(m.entries))))
	lines = append(lines, "")
	if len(m.entries) == 0 {
		lines = append(lines, dashMuted.Render("(none)"))
		return strings.Join(lines, "\n")
	}
	for i, entry := range m.entries {
		cursor := " "
		rowStyle := lipgloss.NewStyle()
		if i == m.selected {
			cursor = ">"
			rowStyle = dashSelected
		}
		status := statusBadge(entry.Status)
		owner := coalesce(entry.Owner, "-")
		line := fmt.Sprintf("%s %-12s %-10s snaps=%-2d %s", cursor, entry.ID, owner, entry.Snapshots, status)
		lines = append(lines, rowStyle.Render(truncateLine(line, width)))
		lines = append(lines, dashMuted.Render(truncateLine(fmt.Sprintf("  +%d -%d  %s", entry.DiffStat.Insertions, entry.DiffStat.Deletions, coalesce(entry.Purpose, "-")), width)))
	}
	return strings.Join(lines, "\n")
}

func (m dashModel) renderPreview(width, height int) string {
	lines := []string{dashHeader.Render("Preview"), ""}
	if m.preview == nil {
		lines = append(lines, dashMuted.Render("Select an agent to inspect."))
		return fitLines(lines, width, height)
	}

	summary := m.preview.Summary
	lines = append(lines,
		fmt.Sprintf("id: %s", summary.ID),
		fmt.Sprintf("status: %s", summary.Status),
		fmt.Sprintf("owner: %s", coalesce(summary.Owner, "-")),
		fmt.Sprintf("branch: %s", summary.Branch),
		fmt.Sprintf("path: %s", summary.Path),
		"",
		dashHeader.Render("Current"),
	)
	if len(m.preview.CurrentChanges) == 0 {
		lines = append(lines, dashMuted.Render("  clean relative to latest snapshot/base"))
	} else {
		for _, change := range m.preview.CurrentChanges {
			lines = append(lines, fmt.Sprintf("  %-4s %s", change.Status, change.Path))
		}
	}

	lines = append(lines, "", dashHeader.Render("Snapshots"))
	if len(m.preview.Snapshots) == 0 {
		lines = append(lines, dashMuted.Render("  none"))
	} else {
		for _, snapshot := range m.preview.Snapshots {
			lines = append(lines, fmt.Sprintf("  %-8s %s", snapshot.Name, truncateLine(snapshot.Message, max(12, width-14))))
		}
	}
	if m.preview.Stop != nil {
		lines = append(lines, "", dashHeader.Render("Stop"), fmt.Sprintf("  %s", coalesce(m.preview.Stop.Reason, "-")))
	}
	return fitLines(lines, width, height)
}

func (m dashModel) renderDetailHeader() string {
	if m.detail == nil {
		return "No detail loaded"
	}
	s := m.detail.Summary
	return fmt.Sprintf("%s  %s  snaps=%d  current=%d  locked=%t",
		s.ID, statusBadge(s.Status), s.Snapshots, len(m.detail.CurrentChanges), m.detail.Locked)
}

func (m dashModel) renderSnapshotsPane(width, height int) string {
	snapshotCount := 0
	if m.detail != nil {
		snapshotCount = len(m.detail.Snapshots)
	}
	lines := []string{dashHeader.Render(fmt.Sprintf("Snapshots (%d)", snapshotCount))}
	if m.focus == focusSnapshots {
		lines[0] += " " + dashAccent.Render("[focus]")
	}
	lines = append(lines, "")
	if m.detail == nil || len(m.detail.Snapshots) == 0 {
		lines = append(lines, dashMuted.Render("(none)"))
		return fitLines(lines, width, height)
	}
	for i, snapshot := range m.detail.Snapshots {
		prefix := " "
		style := lipgloss.NewStyle()
		if i == m.snapshotIndex {
			prefix = ">"
			style = dashSelected
		}
		lines = append(lines, style.Render(truncateLine(fmt.Sprintf("%s %-8s %s", prefix, snapshot.Name, formatTimestamp(snapshot.Timestamp)), width)))
		lines = append(lines, dashMuted.Render(truncateLine(fmt.Sprintf("  %s", snapshot.Commit), width)))
	}
	return fitLines(lines, width, height)
}

func (m dashModel) renderFilesPane(width, height int) string {
	lines := []string{dashHeader.Render(fmt.Sprintf("Files (%d)", len(m.currentChanges())))}
	if m.focus == focusFiles {
		lines[0] += " " + dashAccent.Render("[focus]")
	}
	lines = append(lines, "")
	changes := m.currentChanges()
	if len(changes) == 0 {
		lines = append(lines, dashMuted.Render("(none)"))
		return fitLines(lines, width, height)
	}
	for i, change := range changes {
		prefix := " "
		style := lipgloss.NewStyle()
		if i == m.fileIndex {
			prefix = ">"
			style = dashSelected
		}
		lines = append(lines, style.Render(truncateLine(fmt.Sprintf("%s %-4s %s", prefix, change.Status, change.Path), width)))
	}
	return fitLines(lines, width, height)
}

func (m dashModel) renderInspectorPane(width, height int) string {
	lines := []string{dashHeader.Render("Inspector"), ""}
	if m.detail == nil {
		lines = append(lines, dashMuted.Render("No detail loaded"))
		return fitLines(lines, width, height)
	}

	snapshot := m.currentSnapshot()
	if snapshot != nil {
		lines = append(lines,
			fmt.Sprintf("snapshot: %s", snapshot.Name),
			fmt.Sprintf("commit: %s", snapshot.Commit),
			fmt.Sprintf("parent: %s", coalesce(snapshot.Parent, "-")),
			fmt.Sprintf("time: %s", formatTimestamp(snapshot.Timestamp)),
			"",
			dashHeader.Render("Message"),
			snapshot.Message,
		)
	}
	if len(m.detail.CurrentChanges) > 0 {
		lines = append(lines, "", dashHeader.Render("Unsnapshotted"))
		for _, change := range m.detail.CurrentChanges {
			lines = append(lines, fmt.Sprintf("  %-4s %s", change.Status, change.Path))
		}
	}
	if m.detail.Stop != nil {
		lines = append(lines, "", dashHeader.Render("Stop"))
		lines = append(lines, m.detail.Stop.Reason)
	}
	lines = append(lines, "", dashHeader.Render("Actions"))
	if m.detail.Summary.Status == "active" {
		lines = append(lines, "  s  stop")
	} else if m.detail.Summary.Status == "stopped" {
		lines = append(lines, "  u  resume")
	}
	if m.currentSnapshot() != nil {
		lines = append(lines, "  r  rollback to selected snapshot")
	}
	lines = append(lines, "  D  finalize selected agent", "  x  abort selected agent")
	return fitLines(lines, width, height)
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

func tickDashboard(seconds int) tea.Cmd {
	if seconds <= 0 {
		seconds = dashDefaultRefreshSeconds
	}
	return tea.Tick(time.Duration(seconds)*time.Second, func(time.Time) tea.Msg {
		return refreshMsg{}
	})
}

func statusBadge(status string) string {
	switch status {
	case "active":
		return dashAccent.Render("active")
	case "stopped":
		return dashWarn.Render("stopped")
	case "orphaned":
		return dashStop.Render("orphaned")
	default:
		return dashMuted.Render(status)
	}
}

func fitText(body string, width, height int) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	return fitLines(lines, width, height)
}

func fitLines(lines []string, width, height int) string {
	if height <= 0 {
		height = len(lines)
	}
	out := make([]string, 0, min(len(lines), height))
	for i, line := range lines {
		if i >= height {
			break
		}
		out = append(out, truncateLine(line, width))
	}
	return strings.Join(out, "\n")
}

func truncateLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func coalesce(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func listStatusLine() string {
	return "[↑/↓] move  [→/enter] detail  [r] refresh  [q] quit"
}
