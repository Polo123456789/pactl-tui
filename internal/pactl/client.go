package pactl

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strings"

	audiomodel "github.com/pablo/pactl-tui/internal/model"
)

type Client struct {
	bin string
}

func New(bin string) *Client {
	return &Client{bin: bin}
}

func (c *Client) State(ctx context.Context) (audiomodel.Snapshot, error) {
	var info rawInfo
	var sinks []rawDevice
	var sources []rawDevice
	var cards []rawCard

	if err := c.runJSON(ctx, &info, "-f", "json", "info"); err != nil {
		return audiomodel.Snapshot{}, err
	}
	if err := c.runJSON(ctx, &sinks, "-f", "json", "list", "sinks"); err != nil {
		return audiomodel.Snapshot{}, err
	}
	if err := c.runJSON(ctx, &sources, "-f", "json", "list", "sources"); err != nil {
		return audiomodel.Snapshot{}, err
	}
	if err := c.runJSON(ctx, &cards, "-f", "json", "list", "cards"); err != nil {
		return audiomodel.Snapshot{}, err
	}

	cardMap := make(map[string]audiomodel.Card, len(cards))
	cardByID := make(map[string]audiomodel.Card, len(cards))
	for _, card := range cards {
		mapped := mapCard(card)
		cardMap[mapped.Name] = mapped
		cardByID[fmt.Sprintf("%d", card.Index)] = mapped
	}

	snapshot := audiomodel.Snapshot{
		Info: audiomodel.ServerInfo{
			ServerName:    info.ServerName,
			ServerVersion: info.ServerVersion,
			DefaultSink:   info.DefaultSink,
			DefaultSource: info.DefaultSource,
		},
		Sinks:   make([]audiomodel.Device, 0, len(sinks)),
		Sources: make([]audiomodel.Device, 0, len(sources)),
		Cards:   cardMap,
	}

	for _, sink := range sinks {
		snapshot.Sinks = append(snapshot.Sinks, mapDevice(audiomodel.SinkKind, sink, info.DefaultSink, cardMap, cardByID))
	}
	for _, source := range sources {
		snapshot.Sources = append(snapshot.Sources, mapDevice(audiomodel.SourceKind, source, info.DefaultSource, cardMap, cardByID))
	}

	return snapshot, nil
}

func (c *Client) SetDefaultSink(ctx context.Context, name string) error {
	return c.run(ctx, "set-default-sink", name)
}

func (c *Client) SetDefaultSource(ctx context.Context, name string) error {
	return c.run(ctx, "set-default-source", name)
}

func (c *Client) AdjustSinkVolume(ctx context.Context, name string, delta int) error {
	return c.run(ctx, "set-sink-volume", name, fmt.Sprintf("%+d%%", delta))
}

func (c *Client) AdjustSourceVolume(ctx context.Context, name string, delta int) error {
	return c.run(ctx, "set-source-volume", name, fmt.Sprintf("%+d%%", delta))
}

func (c *Client) ToggleSinkMute(ctx context.Context, name string) error {
	return c.run(ctx, "set-sink-mute", name, "toggle")
}

func (c *Client) ToggleSourceMute(ctx context.Context, name string) error {
	return c.run(ctx, "set-source-mute", name, "toggle")
}

func (c *Client) SetCardProfile(ctx context.Context, cardName, profileName string) error {
	return c.run(ctx, "set-card-profile", cardName, profileName)
}

func (c *Client) runJSON(ctx context.Context, out any, args ...string) error {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	data, err := cmd.Output()
	if err != nil {
		return commandError(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse pactl json: %w", err)
	}
	return nil
}

func (c *Client) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return commandError(err)
		}
		return fmt.Errorf("pactl %s: %s", strings.Join(args, " "), trimmed)
	}
	return nil
}

func commandError(err error) error {
	if exitErr, ok := err.(*exec.ExitError); ok {
		trimmed := strings.TrimSpace(string(exitErr.Stderr))
		if trimmed != "" {
			return fmt.Errorf("pactl: %s", trimmed)
		}
	}
	return fmt.Errorf("run pactl: %w", err)
}

type rawInfo struct {
	ServerName    string `json:"server_name"`
	ServerVersion string `json:"server_version"`
	DefaultSink   string `json:"default_sink_name"`
	DefaultSource string `json:"default_source_name"`
}

type rawDevice struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	State       string                    `json:"state"`
	Mute        bool                      `json:"mute"`
	Volume      map[string]rawVolumeEntry `json:"volume"`
	ActivePort  string                    `json:"active_port"`
	MonitorOf   string                    `json:"monitor_of_sink"`
	Properties  map[string]string         `json:"properties"`
}

type rawVolumeEntry struct {
	Value int `json:"value"`
}

type rawCard struct {
	Index         int                   `json:"index"`
	Name          string                `json:"name"`
	ActiveProfile string                `json:"active_profile"`
	Profiles      map[string]rawProfile `json:"profiles"`
	Properties    map[string]string     `json:"properties"`
}

type rawProfile struct {
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	Available   bool   `json:"available"`
}

func mapCard(card rawCard) audiomodel.Card {
	profiles := make([]audiomodel.Profile, 0, len(card.Profiles))
	for name, profile := range card.Profiles {
		profiles = append(profiles, audiomodel.Profile{
			Name:        name,
			Description: profile.Description,
			Priority:    profile.Priority,
			Available:   profile.Available,
			IsActive:    name == card.ActiveProfile,
		})
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].IsActive != profiles[j].IsActive {
			return profiles[i].IsActive
		}
		if profiles[i].Available != profiles[j].Available {
			return profiles[i].Available
		}
		if profiles[i].Priority != profiles[j].Priority {
			return profiles[i].Priority > profiles[j].Priority
		}
		return profiles[i].Name < profiles[j].Name
	})

	return audiomodel.Card{
		Name:          card.Name,
		Description:   firstNonEmpty(card.Properties["device.description"], card.Name),
		ActiveProfile: card.ActiveProfile,
		Profiles:      profiles,
	}
}

func mapDevice(kind audiomodel.DeviceKind, device rawDevice, defaultName string, cardsByName, cardsByID map[string]audiomodel.Card) audiomodel.Device {
	cardName, cardLabel := resolveCard(device, cardsByName, cardsByID)

	isMonitor := false
	if kind == audiomodel.SourceKind {
		isMonitor = isMonitorSource(device)
	}

	return audiomodel.Device{
		Kind:          kind,
		Name:          device.Name,
		Description:   firstNonEmpty(device.Description, device.Name),
		State:         device.State,
		VolumePercent: volumePercent(device.Volume),
		Mute:          device.Mute,
		IsDefault:     device.Name == defaultName,
		CardName:      cardName,
		CardLabel:     firstNonEmpty(cardLabel, cardName),
		ActivePort:    device.ActivePort,
		IsMonitor:     isMonitor,
	}
}

func isMonitorSource(device rawDevice) bool {
	if device.MonitorOf != "" && device.MonitorOf != "n/a" {
		return true
	}
	if strings.HasSuffix(device.Name, ".monitor") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(device.Properties["device.class"]), "monitor") {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(device.Description), "Monitor of ") {
		return true
	}
	return false
}

func resolveCard(device rawDevice, cardsByName, cardsByID map[string]audiomodel.Card) (string, string) {
	if cardID := strings.TrimSpace(device.Properties["device.id"]); cardID != "" {
		if card, ok := cardsByID[cardID]; ok {
			return card.Name, card.Description
		}
	}

	if cardName := strings.TrimSpace(device.Properties["device.name"]); cardName != "" {
		if card, ok := cardsByName[cardName]; ok {
			return card.Name, card.Description
		}
		return cardName, cardName
	}

	return "", ""
}

func volumePercent(entries map[string]rawVolumeEntry) int {
	if len(entries) == 0 {
		return 0
	}

	total := 0.0
	count := 0.0
	for _, entry := range entries {
		total += float64(entry.Value)
		count++
	}

	return int(math.Round((total / count) * 100 / 65536))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
