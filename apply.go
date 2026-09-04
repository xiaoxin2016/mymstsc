//go:build windows

package main

import (
	"strings"
)

// applier writes a Config onto a control, tolerating members that a given
// Windows build does not expose.
type applier struct {
	c        *rdpControl
	warnings []string
}

// set writes a value, downgrading "member not supported here" to a warning.
func (a *applier) set(what string, value interface{}, paths ...string) {
	err := a.c.setAny(value, paths...)
	if err == nil {
		return
	}
	if isNotSupported(err) {
		msg := what + " is not supported by " + a.c.coclass.Type.Name
		a.warnings = append(a.warnings, msg)
		logWarnf("%s (%v)", msg, err)
		return
	}
	msg := what + " could not be applied: " + err.Error()
	a.warnings = append(a.warnings, msg)
	logWarnf("%s", msg)
}

// must writes a value that the connection cannot do without.
func (a *applier) must(what string, value interface{}, paths ...string) error {
	if err := a.c.setAny(value, paths...); err != nil {
		return errf("%s: %w", what, err)
	}
	return nil
}

// applySettings pushes the whole configuration onto the control. It runs before
// Connect(), because most of these properties are read once at connect time.
func applySettings(c *rdpControl, cfg *Config) ([]string, error) {
	a := &applier{c: c}

	if err := a.must("server name", cfg.Server, "Server"); err != nil {
		return a.warnings, err
	}
	if cfg.Port != 0 {
		a.set("server port", int32(cfg.Port), "advanced.RDPPort")
	}
	if cfg.Username != "" {
		a.set("user name", cfg.Username, "UserName")
	}
	if cfg.Domain != "" {
		a.set("domain", cfg.Domain, "Domain")
	}

	if err := a.must("desktop width", int32(cfg.Width), "DesktopWidth"); err != nil {
		return a.warnings, err
	}
	if err := a.must("desktop height", int32(cfg.Height), "DesktopHeight"); err != nil {
		return a.warnings, err
	}
	if cfg.ColorDepth != 0 {
		a.set("color depth", int32(cfg.ColorDepth), "ColorDepth")
	}

	// Keyboard and focus behaviour: without these the control never sees the
	// Windows key or Alt-Tab, which is the most visible difference from mstsc.
	a.set("focus grab", true, "advanced.GrabFocusOnConnect")
	a.set("Windows key passthrough", true, "advanced.EnableWindowsKey")

	if cfg.ConnectionBar != nil {
		a.set("connection bar", *cfg.ConnectionBar, "advanced.DisplayConnectionBar")
	}
	if cfg.PinConnBar != nil {
		a.set("connection bar pinning", *cfg.PinConnBar, "advanced.PinConnectionBar")
	}
	if cfg.SmartSizing != nil {
		a.set("smart sizing", *cfg.SmartSizing, "advanced.SmartSizing")
	}

	// Security.
	if cfg.AuthLevel != nil {
		a.set("authentication level", uint32(*cfg.AuthLevel), "advanced.AuthenticationLevel")
	}
	if cfg.EnableCredSSP != nil {
		a.set("CredSSP", *cfg.EnableCredSSP, "advanced.EnableCredSspSupport")
	}
	if cfg.NegotiateLayer != nil {
		a.set("security layer negotiation", *cfg.NegotiateLayer, "advanced.NegotiateSecurityLayer")
	}
	if cfg.AdminSession {
		a.set("administrative session", true,
			"advanced.ConnectToAdministerServer", "advanced.ConnectToServerConsole")
	}
	if cfg.RestrictedAdmin {
		a.set("Restricted Admin mode", true,
			"advanced.RestrictedLogon", "nonscriptable.RestrictedLogon",
			"advanced.DisableCredentialsDelegation")
	}
	if cfg.RemoteGuard {
		a.set("Remote Credential Guard", true,
			"advanced.RedirectedAuthentication", "nonscriptable.RedirectedAuthentication")
	}
	if cfg.Public {
		a.set("public mode", true, "advanced.PublicMode", "nonscriptable.PublicMode")
	}

	// Redirection.
	if cfg.RedirectDrives != nil {
		a.set("drive redirection", *cfg.RedirectDrives, "advanced.RedirectDrives")
	}
	if cfg.RedirectPrinters != nil {
		a.set("printer redirection", *cfg.RedirectPrinters, "advanced.RedirectPrinters")
	}
	if cfg.RedirectPorts != nil {
		a.set("COM port redirection", *cfg.RedirectPorts, "advanced.RedirectPorts")
	}
	if cfg.RedirectSmartCards != nil {
		a.set("smart card redirection", *cfg.RedirectSmartCards, "advanced.RedirectSmartCards")
	}
	if cfg.RedirectClipboard != nil {
		a.set("clipboard redirection", *cfg.RedirectClipboard, "advanced.RedirectClipboard")
	}
	if cfg.RedirectPOS != nil {
		a.set("POS device redirection", *cfg.RedirectPOS, "advanced.RedirectPOSDevices")
	}
	if strings.TrimSpace(cfg.DrivesToRedirect) != "" &&
		!strings.EqualFold(strings.TrimSpace(cfg.DrivesToRedirect), "*") {
		w := "drivestoredirect selects individual drives (" + cfg.DrivesToRedirect +
			"); this build redirects either all drives or none"
		a.warnings = append(a.warnings, w)
		logWarnf("%s", w)
	}

	// Audio and keyboard.
	if cfg.AudioMode != nil {
		a.set("audio redirection", uint32(*cfg.AudioMode),
			"secured.AudioRedirectionMode", "advanced.AudioRedirectionMode")
	}
	if cfg.AudioCaptureMode != nil {
		a.set("audio capture", uint32(*cfg.AudioCaptureMode), "advanced.AudioCaptureRedirectionMode")
	}
	if cfg.AudioQuality != nil {
		a.set("audio quality", uint32(*cfg.AudioQuality), "advanced.AudioQualityMode")
	}
	if cfg.KeyboardHook != nil {
		a.set("keyboard hook", int32(*cfg.KeyboardHook), "secured.KeyboardHookMode")
	}

	// Performance.
	if cfg.Compression != nil {
		a.set("compression", *cfg.Compression, "advanced.Compress")
	}
	if cfg.PerformanceFlags != nil {
		a.set("performance flags", uint32(*cfg.PerformanceFlags), "advanced.PerformanceFlags")
	}
	if cfg.AutoReconnect != nil {
		a.set("automatic reconnection", *cfg.AutoReconnect, "advanced.EnableAutoReconnect")
	}
	if cfg.MaxReconnect != nil {
		a.set("reconnect attempts", int32(*cfg.MaxReconnect), "advanced.MaxReconnectAttempts")
	}
	if cfg.NetworkAutoDetect != nil {
		a.set("network auto detect", *cfg.NetworkAutoDetect, "advanced.NetworkAutoDetect")
	}
	if cfg.BandwidthDetect != nil {
		a.set("bandwidth detection", *cfg.BandwidthDetect, "advanced.BandwidthDetection")
	}
	if cfg.NetworkType != nil {
		a.set("connection type", uint32(*cfg.NetworkType), "advanced.NetworkConnectionType")
	}

	// Session start-up program.
	if cfg.StartProgram != "" {
		a.set("alternate shell", cfg.StartProgram, "secured.StartProgram")
	}
	if cfg.WorkDir != "" {
		a.set("working directory", cfg.WorkDir, "secured.WorkDir")
	}
	if cfg.LoadBalanceInfo != "" {
		a.set("load balance info", cfg.LoadBalanceInfo, "advanced.LoadBalanceInfo")
	}

	// Multiple monitors.
	if cfg.MultiMon {
		a.set("multiple monitors", true, "nonscriptable.UseMultimon")
	}
	if cfg.Span {
		a.set("monitor spanning", true, "nonscriptable.SpanMonitors", "advanced.SpanMonitors")
	}

	// RD Gateway.
	applyGateway(a, &cfg.Gateway)

	// Credentials last, so a failure here is reported after everything else is
	// in place.
	if cfg.PasswordSet {
		if err := c.setAny(cfg.Password,
			"advanced.ClearTextPassword", "nonscriptable.ClearTextPassword"); err != nil {
			w := "the password could not be handed to the control (" + err.Error() +
				"); Windows will ask for credentials instead"
			a.warnings = append(a.warnings, w)
			logWarnf("%s", w)
		} else {
			logDebugf("password supplied through ClearTextPassword")
		}
	}
	if cfg.PromptForCreds || !cfg.PasswordSet {
		// Let the control raise the standard Windows credential prompt rather
		// than failing the connection outright.
		a.set("credential prompt", true,
			"nonscriptable.PromptForCredsOnClient", "nonscriptable.PromptForCredentials")
	}

	// Raw overrides come last so they win over everything above.
	for _, o := range cfg.Overrides {
		path := o.Name
		if o.Target != "" {
			path = o.Target + "." + o.Name
		}
		if err := c.setAny(o.typedValue(), path); err != nil {
			w := "/set:" + o.Raw + " failed: " + err.Error()
			a.warnings = append(a.warnings, w)
			logWarnf("%s", w)
		} else {
			logInfof("override %s applied", path)
		}
	}

	return a.warnings, nil
}

func applyGateway(a *applier, g *gatewayConfig) {
	if g.Hostname == "" && g.UsageMethod == nil {
		return
	}
	if g.Hostname != "" {
		a.set("gateway host name", g.Hostname, "transport.GatewayHostname")
		if g.UsageMethod == nil {
			// 1 = always use the gateway (TSC_PROXY_MODE_DIRECT).
			g.UsageMethod = intPtr(1)
		}
	}
	if g.UsageMethod != nil {
		a.set("gateway usage method", uint32(*g.UsageMethod), "transport.GatewayUsageMethod")
	}
	if g.ProfileUsage != nil {
		a.set("gateway profile usage", uint32(*g.ProfileUsage), "transport.GatewayProfileUsageMethod")
	}
	if g.CredsSource != nil {
		a.set("gateway credential source", uint32(*g.CredsSource), "transport.GatewayCredsSource")
	}
	if g.Username != "" {
		a.set("gateway user name", g.Username, "transport.GatewayUsername")
	}
	if g.Domain != "" {
		a.set("gateway domain", g.Domain, "transport.GatewayDomain")
	}
	if g.PasswordSet {
		a.set("gateway password", g.Password, "transport.GatewayPassword")
	}
	if g.CredsSharing != nil {
		a.set("gateway credential sharing", *g.CredsSharing, "transport.GatewayCredSharing")
	}
}
