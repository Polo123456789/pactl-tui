package model

type DeviceKind string

const (
	SinkKind   DeviceKind = "sink"
	SourceKind DeviceKind = "source"
)

type ServerInfo struct {
	ServerName    string
	ServerVersion string
	DefaultSink   string
	DefaultSource string
}

type Profile struct {
	Name        string
	Description string
	Priority    int
	Available   bool
	IsActive    bool
}

type Card struct {
	Name          string
	Description   string
	ActiveProfile string
	Profiles      []Profile
}

type Device struct {
	Kind          DeviceKind
	Name          string
	Description   string
	State         string
	VolumePercent int
	Mute          bool
	IsDefault     bool
	CardName      string
	CardLabel     string
	ActivePort    string
	IsMonitor     bool
}

type Snapshot struct {
	Info    ServerInfo
	Sinks   []Device
	Sources []Device
	Cards   map[string]Card
}

func (s Snapshot) VisibleSources(showMonitors bool) []Device {
	if showMonitors {
		return s.Sources
	}

	visible := make([]Device, 0, len(s.Sources))
	for _, source := range s.Sources {
		if !source.IsMonitor {
			visible = append(visible, source)
		}
	}

	return visible
}
