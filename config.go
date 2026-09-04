package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Optional (tri-state) settings: nil means "leave the control's default alone",
// which matters because .rdp files and the command line only mention a subset.
func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

// gatewayConfig holds Remote Desktop Gateway settings.
type gatewayConfig struct {
	Hostname     string
	UsageMethod  *int // 0 none, 1 direct, 2 detect, 4 default
	CredsSource  *int // 0 password, 1 smart card, 4 any
	ProfileUsage *int
	Username     string
	Domain       string
	Password     string
	PasswordSet  bool
	CredsSharing *bool // reuse the session credentials for the gateway
}

// propOverride is one raw property assignment from /set:.
type propOverride struct {
	Target string // "", "advanced", "secured", "transport", "nonscriptable"
	Name   string
	Value  string
	Raw    string
}

// Config is the fully resolved set of options for one connection. It is built
// from an optional .rdp file first, then overlaid with command-line switches.
type Config struct {
	Server   string
	Port     int
	Username string
	Domain   string
	Password string
	// PasswordSet distinguishes "no password given" from "empty password".
	PasswordSet bool

	Width      int
	Height     int
	FullScreen bool
	Span       bool
	MultiMon   bool
	ColorDepth int

	SmartSizing   *bool
	DynamicResize bool

	// Scale is the desktop scale factor in percent, or scaleAuto to follow the
	// DPI of the monitor. DeviceScale is the matching device scale factor, or
	// scaleAuto to derive it from Scale.
	Scale         int
	DeviceScale   int
	ConnectionBar *bool
	PinConnBar    *bool

	AdminSession    bool
	RestrictedAdmin bool
	RemoteGuard     bool
	Public          bool
	PromptForCreds  bool

	AuthLevel      *int
	EnableCredSSP  *bool
	NegotiateLayer *bool

	RedirectDrives     *bool
	DrivesToRedirect   string
	RedirectPrinters   *bool
	RedirectClipboard  *bool
	RedirectPorts      *bool
	RedirectSmartCards *bool
	RedirectPOS        *bool

	AudioMode        *int
	AudioCaptureMode *int
	AudioQuality     *int
	KeyboardHook     *int

	Compression       *bool
	PerformanceFlags  *int
	AutoReconnect     *bool
	MaxReconnect      *int
	NetworkAutoDetect *bool
	BandwidthDetect   *bool
	NetworkType       *int

	StartProgram string
	WorkDir      string

	Gateway         gatewayConfig
	LoadBalanceInfo string

	Title     string
	RDPFile   string
	Overrides []propOverride

	// Behaviour of this program rather than of the control.
	CoclassName string // force a specific coclass, e.g. MsRdpClient9NotSafeForScripting
	ListClasses bool
	LogLevel    logLevel
	Timestamps  bool
	ReadPwStdin bool
}

func newConfig() *Config {
	return &Config{
		Port:          0, // 0 = leave the control default (3389)
		ColorDepth:    0,
		DynamicResize: true,
		LogLevel:      logInfo,
	}
}

// Validate checks the configuration is usable and fills in derived defaults.
func (c *Config) Validate() error {
	if c.ListClasses {
		return nil
	}
	if strings.TrimSpace(c.Server) == "" {
		return errf("no server specified (use /v:<server[:port]> or pass a .rdp file)")
	}
	if c.Port < 0 || c.Port > 65535 {
		return errf("invalid port %d", c.Port)
	}
	if c.Width < 0 || c.Height < 0 {
		return errf("invalid resolution %dx%d", c.Width, c.Height)
	}
	if c.Width != 0 && (c.Width < 200 || c.Width > 8192) {
		return errf("width %d out of range (200-8192)", c.Width)
	}
	if c.Height != 0 && (c.Height < 200 || c.Height > 8192) {
		return errf("height %d out of range (200-8192)", c.Height)
	}
	if c.Scale != scaleAuto && (c.Scale < 100 || c.Scale > 500) {
		return errf("scale %d%% out of range (100-500)", c.Scale)
	}
	if c.DeviceScale != scaleAuto {
		ok := false
		for _, v := range deviceScaleBuckets {
			ok = ok || v == c.DeviceScale
		}
		if !ok {
			return errf("device scale must be one of %v, got %d", deviceScaleBuckets, c.DeviceScale)
		}
	}
	switch c.ColorDepth {
	case 0, 8, 15, 16, 24, 32:
	default:
		return errf("unsupported color depth %d (use 8, 15, 16, 24 or 32)", c.ColorDepth)
	}
	if c.MultiMon && c.Span {
		return errf("/span and /multimon are mutually exclusive")
	}
	if (c.MultiMon || c.Span) && !c.FullScreen {
		// mstsc implies full screen for both; match that.
		c.FullScreen = true
	}
	if c.Title == "" {
		c.Title = fmt.Sprintf("%s - %s", c.Server, appName)
	}
	return nil
}

// splitHostPort splits "host:port", "host", "[v6]:port" and "[v6]".
func splitHostPort(s string) (host string, port int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, errf("empty server address")
	}
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", 0, errf("unbalanced '[' in address %q", s)
		}
		host = s[1:end]
		rest := s[end+1:]
		if rest == "" {
			return host, 0, nil
		}
		if !strings.HasPrefix(rest, ":") {
			return "", 0, errf("unexpected %q after address", rest)
		}
		p, perr := strconv.Atoi(rest[1:])
		if perr != nil || p < 1 || p > 65535 {
			return "", 0, errf("invalid port in %q", s)
		}
		return host, p, nil
	}
	// A bare IPv6 literal has more than one colon and no port.
	if strings.Count(s, ":") > 1 {
		return s, 0, nil
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		host = s[:i]
		p, perr := strconv.Atoi(s[i+1:])
		if perr != nil || p < 1 || p > 65535 {
			return "", 0, errf("invalid port in %q", s)
		}
		return host, p, nil
	}
	return s, 0, nil
}

// parseOverride parses "target.Property=value" or "Property=value".
func parseOverride(s string) (propOverride, error) {
	eq := strings.Index(s, "=")
	if eq < 0 {
		return propOverride{}, errf("/set expects Property=Value, got %q", s)
	}
	lhs, value := strings.TrimSpace(s[:eq]), s[eq+1:]
	o := propOverride{Value: value, Raw: s}
	if dot := strings.LastIndex(lhs, "."); dot >= 0 {
		o.Target = strings.ToLower(strings.TrimSpace(lhs[:dot]))
		o.Name = strings.TrimSpace(lhs[dot+1:])
	} else {
		o.Name = lhs
	}
	if o.Name == "" {
		return propOverride{}, errf("/set has an empty property name: %q", s)
	}
	switch o.Target {
	case "", "control", "advanced", "secured", "transport", "nonscriptable":
	default:
		return propOverride{}, errf("/set: unknown target %q (use control, advanced, secured, transport or nonscriptable)", o.Target)
	}
	return o, nil
}

// typedValue converts an override's textual value into the VARIANT-friendly Go
// type implied by its spelling: bare integers become integers, "true"/"false"
// become booleans, everything else stays a string.
func (o propOverride) typedValue() interface{} {
	v := strings.TrimSpace(o.Value)
	switch strings.ToLower(v) {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(v); err == nil {
		return int32(n)
	}
	if strings.HasPrefix(v, "s:") { // force a string
		return o.Value[2:]
	}
	return o.Value
}
