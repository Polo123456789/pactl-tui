package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	audiomodel "github.com/pablo/pactl-tui/internal/model"
	"github.com/pablo/pactl-tui/internal/pactl"
)

const (
	volumeStep = 5
	maxVolume  = 150
)

var (
	appStyle = lipgloss.NewStyle().Padding(0, 1)

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	tabStyle    = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("245"))
	activeTab   = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))

	panelStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	selectedItem     = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
	mutedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	defaultStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	defaultBadge     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color("42")).Padding(0, 1)
	defaultLineStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	secondaryStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	statusOKStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	statusErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	helpStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	profileStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
)

type loadedMsg struct {
	snapshot audiomodel.Snapshot
	status   string
	request  uint64
}

type errMsg struct {
	err     error
	request uint64
}

type model struct {
	client *pactl.Client

	snapshot     audiomodel.Snapshot
	activeTab    audiomodel.DeviceKind
	sinkIndex    int
	sourceIndex  int
	profileIndex int

	selectedSinkName   string
	selectedSourceName string

	profileMode  bool
	showMonitors bool
	loading      bool
	status       string
	statusIsErr  bool
	width        int
	height       int
	requestID    uint64
}

func New(client *pactl.Client) tea.Model {
	return model{
		client:       client,
		activeTab:    audiomodel.SinkKind,
		loading:      true,
		showMonitors: false,
		status:       "Cargando estado de audio...",
	}
}

func (m model) Init() tea.Cmd {
	return loadStateCmd(m.client, 0, "Estado de audio cargado")
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case loadedMsg:
		if msg.request != m.requestID {
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.loading = false
		m.status = msg.status
		m.statusIsErr = false
		m.restoreSelection()
		return m, nil

	case errMsg:
		if msg.request != m.requestID {
			return m, nil
		}
		m.loading = false
		m.status = msg.err.Error()
		m.statusIsErr = true
		m.restoreSelection()
		return m, nil

	case tea.KeyMsg:
		if m.profileMode {
			return m.handleProfileKeys(msg)
		}
		return m.handleMainKeys(msg)
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Cargando interfaz..."
	}

	contentWidth := maxInt(20, m.width-appStyle.GetHorizontalFrameSize())
	contentHeight := maxInt(8, m.height-appStyle.GetVerticalFrameSize())

	header := strings.Join([]string{
		headerStyle.Render("pactl-tui"),
		secondaryStyle.Render(firstNonEmpty(m.snapshot.Info.ServerName, "Audio server")),
		secondaryStyle.Render(fmt.Sprintf("Default sink: %s", shortName(m.snapshot.Info.DefaultSink))),
		secondaryStyle.Render(fmt.Sprintf("Default source: %s", shortName(m.snapshot.Info.DefaultSource))),
	}, "  ")

	tabs := m.renderTabs()
	footer := m.renderFooter()
	status := m.renderStatus()
	reservedHeight := lipgloss.Height(header) + lipgloss.Height(tabs) + lipgloss.Height(status) + lipgloss.Height(footer)
	bodyHeight := maxInt(6, contentHeight-reservedHeight)
	body := m.renderBody(contentWidth, bodyHeight)

	content := lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, status, footer)
	return appStyle.Render(content)
}

func (m model) handleMainKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading {
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab":
		if m.activeTab == audiomodel.SinkKind {
			m.activeTab = audiomodel.SourceKind
		} else {
			m.activeTab = audiomodel.SinkKind
		}
		m.restoreSelection()
		return m, nil
	case "up", "k":
		m.moveSelection(-1)
		return m, nil
	case "down", "j":
		m.moveSelection(1)
		return m, nil
	case "enter":
		device, ok := m.currentDevice()
		if !ok {
			return m, nil
		}
		m.loading = true
		m.requestID++
		if device.Kind == audiomodel.SinkKind {
			return m, actionCmd(m.client, m.requestID, fmt.Sprintf("Sink por defecto: %s", device.Description), func(ctx context.Context) error {
				return m.client.SetDefaultSink(ctx, device.Name)
			})
		}
		return m, actionCmd(m.client, m.requestID, fmt.Sprintf("Source por defecto: %s", device.Description), func(ctx context.Context) error {
			return m.client.SetDefaultSource(ctx, device.Name)
		})
	case "left", "h", "-":
		return m.adjustVolume(-volumeStep)
	case "right", "l", "+":
		return m.adjustVolume(volumeStep)
	case "m":
		device, ok := m.currentDevice()
		if !ok {
			return m, nil
		}
		m.loading = true
		m.requestID++
		if device.Kind == audiomodel.SinkKind {
			return m, actionCmd(m.client, m.requestID, fmt.Sprintf("Mute alternado: %s", device.Description), func(ctx context.Context) error {
				return m.client.ToggleSinkMute(ctx, device.Name)
			})
		}
		return m, actionCmd(m.client, m.requestID, fmt.Sprintf("Mute alternado: %s", device.Description), func(ctx context.Context) error {
			return m.client.ToggleSourceMute(ctx, device.Name)
		})
	case "p":
		card, ok := m.currentCard()
		if !ok || len(card.Profiles) == 0 {
			m.status = "El dispositivo seleccionado no tiene perfiles disponibles"
			m.statusIsErr = true
			return m, nil
		}
		m.profileMode = true
		m.profileIndex = activeProfileIndex(card)
		return m, nil
	case "r":
		m.loading = true
		m.requestID++
		return m, loadStateCmd(m.client, m.requestID, "Estado actualizado")
	case "o":
		m.showMonitors = !m.showMonitors
		m.restoreSelection()
		return m, nil
	}

	return m, nil
}

func (m model) handleProfileKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	card, ok := m.currentCard()
	if !ok || len(card.Profiles) == 0 {
		m.profileMode = false
		return m, nil
	}

	switch msg.String() {
	case "esc", "q":
		m.profileMode = false
		return m, nil
	case "up", "k":
		if m.profileIndex > 0 {
			m.profileIndex--
		}
		return m, nil
	case "down", "j":
		if m.profileIndex < len(card.Profiles)-1 {
			m.profileIndex++
		}
		return m, nil
	case "enter":
		profile := card.Profiles[m.profileIndex]
		if !profile.Available {
			m.status = fmt.Sprintf("Perfil no disponible: %s", profile.Name)
			m.statusIsErr = true
			return m, nil
		}
		m.profileMode = false
		m.loading = true
		m.requestID++
		return m, actionCmd(m.client, m.requestID, fmt.Sprintf("Perfil cambiado: %s", profile.Description), func(ctx context.Context) error {
			return m.client.SetCardProfile(ctx, card.Name, profile.Name)
		})
	}

	return m, nil
}

func (m model) renderTabs() string {
	sinkLabel := "Sinks"
	if m.activeTab == audiomodel.SinkKind {
		sinkLabel = activeTab.Render(sinkLabel)
	} else {
		sinkLabel = tabStyle.Render(sinkLabel)
	}

	sourceLabel := "Sources"
	if m.showMonitors {
		sourceLabel += " (monitors)"
	}
	if m.activeTab == audiomodel.SourceKind {
		sourceLabel = activeTab.Render(sourceLabel)
	} else {
		sourceLabel = tabStyle.Render(sourceLabel)
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, sinkLabel, " ", sourceLabel)
}

func (m model) renderBody(contentWidth, contentHeight int) string {
	listTitle := "Sinks"
	if m.activeTab == audiomodel.SourceKind {
		listTitle = "Sources"
	}

	listWidth := horizontalListWidth(contentWidth)
	detailWidth := maxInt(24, contentWidth-listWidth-1)
	bodyHeight := maxInt(6, contentHeight)
	panelInnerHeight := maxInt(1, bodyHeight-panelStyle.GetVerticalFrameSize())
	listInnerWidth := maxInt(12, listWidth-panelStyle.GetHorizontalFrameSize())
	detailInnerWidth := maxInt(12, detailWidth-panelStyle.GetHorizontalFrameSize())

	list := panelStyle.Width(listInnerWidth).Height(panelInnerHeight).Render(m.renderListWithTitle(listTitle, listInnerWidth, panelInnerHeight))
	detail := panelStyle.Width(detailInnerWidth).Height(panelInnerHeight).Render(m.renderDetail(detailInnerWidth, panelInnerHeight))

	if contentWidth < 110 {
		stackedWidth := maxInt(20, contentWidth-panelStyle.GetHorizontalFrameSize())
		listHeight := maxInt(3, bodyHeight/2)
		detailHeight := maxInt(3, bodyHeight-listHeight)
		list = panelStyle.Width(stackedWidth).Height(maxInt(1, listHeight-panelStyle.GetVerticalFrameSize())).Render(m.renderListWithTitle(listTitle, stackedWidth, maxInt(1, listHeight-panelStyle.GetVerticalFrameSize())))
		detail = panelStyle.Width(stackedWidth).Height(maxInt(1, detailHeight-panelStyle.GetVerticalFrameSize())).Render(m.renderDetail(stackedWidth, maxInt(1, detailHeight-panelStyle.GetVerticalFrameSize())))
		return lipgloss.JoinVertical(lipgloss.Left, list, detail)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
}

func (m model) renderListWithTitle(title string, panelWidth, panelHeight int) string {
	items := m.currentDevices()
	selected := m.currentIndex()
	width := maxInt(20, panelWidth-4)
	height := maxInt(6, panelHeight-2)

	lines := []string{headerStyle.Render(title)}
	if len(items) == 0 {
		lines = append(lines, secondaryStyle.Render("No hay dispositivos para mostrar"))
		return strings.Join(lines, "\n")
	}

	start, end := windowBounds(len(items), selected, height-1)
	for idx := start; idx < end; idx++ {
		item := items[idx]
		defaultMark := ""
		if item.IsDefault {
			defaultMark = " (D)"
		}

		muteMark := ""
		if item.Mute {
			muteMark = mutedStyle.Render(" [mute]")
		}

		volumeLabel := fmt.Sprintf("%3d%%", item.VolumePercent)
		label := deviceListLabel(item)
		line := fmt.Sprintf("%s %-7s %s%s%s", pointer(idx == selected), volumeLabel, truncate(label, width-18), defaultMark, muteMark)
		if item.IsDefault && idx != selected {
			line = defaultLineStyle.Render(line)
		}
		if idx == selected {
			line = selectedItem.Width(width).Render(line)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m model) renderDetail(panelWidth, panelHeight int) string {
	device, ok := m.currentDevice()
	if !ok {
		return headerStyle.Render("Detalle") + "\n" + secondaryStyle.Render("Selecciona un dispositivo")
	}

	card, hasCard := m.currentCard()
	lines := []string{
		headerStyle.Render(device.Description),
		secondaryStyle.Render(device.Name),
		fmt.Sprintf("Tipo: %s", strings.ToUpper(string(device.Kind))),
		fmt.Sprintf("Estado: %s", device.State),
		fmt.Sprintf("Volumen: %d%%", device.VolumePercent),
		fmt.Sprintf("Mute: %s", onOff(device.Mute)),
	}
	if device.IsDefault {
		lines = append(lines, defaultBadge.Render("DEFAULT"))
	} else {
		lines = append(lines, "Default: no")
	}

	if device.ActivePort != "" {
		lines = append(lines, fmt.Sprintf("Puerto activo: %s", device.ActivePort))
	}
	if device.IsMonitor {
		lines = append(lines, mutedStyle.Render("Es una source monitor"))
	}
	if hasCard {
		lines = append(lines,
			fmt.Sprintf("Card: %s", firstNonEmpty(device.CardLabel, card.Description, card.Name)),
			fmt.Sprintf("Perfil activo: %s", card.ActiveProfile),
		)

		if m.profileMode {
			lines = append(lines, "", headerStyle.Render("Selector de perfiles"))
			for idx, profile := range card.Profiles {
				line := profile.Description
				if !profile.Available {
					line += " [no disponible]"
				}
				if idx == m.profileIndex {
					line = selectedItem.Render(line)
				} else if profile.IsActive {
					line = defaultStyle.Render(line)
				}
				lines = append(lines, line)
			}
		} else {
			lines = append(lines, "", profileStyle.Render("Pulsa p para seleccionar perfil"))
		}
	}

	return fitLines(lines, panelWidth-4, panelHeight-2)
}

func (m model) renderStatus() string {
	status := m.status
	if status == "" {
		status = "Listo"
	}
	if m.loading {
		status = "Procesando..."
	}
	if m.statusIsErr {
		return statusErrorStyle.Render(status)
	}
	return statusOKStyle.Render(status)
}

func (m model) renderFooter() string {
	if m.profileMode {
		return helpStyle.Render("j/k mover  enter aplicar perfil  esc cerrar selector")
	}
	return helpStyle.Render("tab cambiar vista  j/k mover  enter default  h/l volumen  m mute  p perfiles  o monitors  r refresh  q salir")
}

func (m *model) moveSelection(delta int) {
	items := m.currentDevices()
	if len(items) == 0 {
		return
	}

	idx := m.currentIndex() + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}

	if m.activeTab == audiomodel.SinkKind {
		m.sinkIndex = idx
		m.selectedSinkName = items[idx].Name
	} else {
		m.sourceIndex = idx
		m.selectedSourceName = items[idx].Name
	}
}

func (m model) adjustVolume(delta int) (tea.Model, tea.Cmd) {
	device, ok := m.currentDevice()
	if !ok {
		return m, nil
	}

	target := clamp(device.VolumePercent+delta, 0, maxVolume)
	if target == device.VolumePercent {
		return m, nil
	}

	m.loading = true
	status := fmt.Sprintf("Volumen ajustado: %s -> %d%%", device.Description, target)
	m.requestID++
	if device.Kind == audiomodel.SinkKind {
		return m, actionCmd(m.client, m.requestID, status, func(ctx context.Context) error {
			return m.client.AdjustSinkVolume(ctx, device.Name, delta)
		})
	}
	return m, actionCmd(m.client, m.requestID, status, func(ctx context.Context) error {
		return m.client.AdjustSourceVolume(ctx, device.Name, delta)
	})
}

func (m *model) restoreSelection() {
	m.sinkIndex = findDeviceIndex(m.snapshot.Sinks, m.selectedSinkName, m.snapshot.Info.DefaultSink)
	if len(m.snapshot.Sinks) > 0 {
		m.selectedSinkName = m.snapshot.Sinks[m.sinkIndex].Name
	}

	visibleSources := m.snapshot.VisibleSources(m.showMonitors)
	m.sourceIndex = findDeviceIndex(visibleSources, m.selectedSourceName, m.snapshot.Info.DefaultSource)
	if len(visibleSources) > 0 {
		m.selectedSourceName = visibleSources[m.sourceIndex].Name
	}

	if card, ok := m.currentCard(); ok {
		m.profileIndex = activeProfileIndex(card)
	} else {
		m.profileIndex = 0
	}
}

func (m model) currentDevices() []audiomodel.Device {
	if m.activeTab == audiomodel.SinkKind {
		return m.snapshot.Sinks
	}
	return m.snapshot.VisibleSources(m.showMonitors)
}

func (m model) currentDevice() (audiomodel.Device, bool) {
	items := m.currentDevices()
	if len(items) == 0 {
		return audiomodel.Device{}, false
	}
	idx := m.currentIndex()
	if idx < 0 || idx >= len(items) {
		return audiomodel.Device{}, false
	}
	return items[idx], true
}

func (m model) currentCard() (audiomodel.Card, bool) {
	device, ok := m.currentDevice()
	if !ok || device.CardName == "" {
		return audiomodel.Card{}, false
	}
	card, ok := m.snapshot.Cards[device.CardName]
	return card, ok
}

func (m model) currentIndex() int {
	if m.activeTab == audiomodel.SinkKind {
		return m.sinkIndex
	}
	return m.sourceIndex
}

func loadStateCmd(client *pactl.Client, request uint64, status string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		snapshot, err := client.State(ctx)
		if err != nil {
			return errMsg{err: err, request: request}
		}
		return loadedMsg{snapshot: snapshot, status: status, request: request}
	}
}

func actionCmd(client *pactl.Client, request uint64, status string, action func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		actionCtx, cancelAction := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelAction()

		if err := action(actionCtx); err != nil {
			return errMsg{err: err, request: request}
		}

		stateCtx, cancelState := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelState()

		snapshot, err := client.State(stateCtx)
		if err != nil {
			return errMsg{err: err, request: request}
		}

		return loadedMsg{snapshot: snapshot, status: status, request: request}
	}
}

func findDeviceIndex(devices []audiomodel.Device, preferred, fallback string) int {
	if len(devices) == 0 {
		return 0
	}

	for idx, device := range devices {
		if device.Name == preferred && preferred != "" {
			return idx
		}
	}
	for idx, device := range devices {
		if device.Name == fallback && fallback != "" {
			return idx
		}
	}
	for idx, device := range devices {
		if device.IsDefault {
			return idx
		}
	}
	return 0
}

func activeProfileIndex(card audiomodel.Card) int {
	for idx, profile := range card.Profiles {
		if profile.IsActive {
			return idx
		}
	}
	return 0
}

func windowBounds(total, selected, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	half := height / 2
	start := selected - half
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > total {
		end = total
		start = end - height
	}
	return start, end
}

func horizontalListWidth(total int) int {
	return maxInt(28, total/3)
}

func pointer(selected bool) string {
	if selected {
		return ">"
	}
	return " "
}

func onOff(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	if width <= 3 {
		if len(runes) < width {
			width = len(runes)
		}
		return string(runes[:width])
	}
	for i := len(runes); i >= 0; i-- {
		candidate := string(runes[:i]) + "..."
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return "..."
}

func shortName(value string) string {
	if value == "" {
		return "-"
	}
	return truncate(value, 32)
}

func deviceListLabel(device audiomodel.Device) string {
	port := cleanPortLabel(device.ActivePort)
	if port != "" && prefersPortLabel(device, port) {
		return port
	}
	return device.Description
}

func prefersPortLabel(device audiomodel.Device, port string) bool {
	description := strings.ToLower(device.Description)
	portLower := strings.ToLower(port)

	if strings.Contains(portLower, "hdmi") || strings.Contains(portLower, "displayport") {
		return true
	}
	if strings.Contains(description, "hdmi / displayport") {
		return true
	}
	if strings.Contains(description, portLower) {
		return true
	}
	return false
}

func cleanPortLabel(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return ""
	}
	if strings.HasPrefix(port, "[") {
		if idx := strings.Index(port, "] "); idx >= 0 {
			port = port[idx+2:]
		}
	}
	return strings.TrimSpace(port)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func fitLines(lines []string, width, height int) string {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = len(lines)
	}

	out := make([]string, 0, minInt(len(lines), height))
	for idx, line := range lines {
		if idx >= height {
			break
		}
		out = append(out, truncate(line, width))
	}
	return strings.Join(out, "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
