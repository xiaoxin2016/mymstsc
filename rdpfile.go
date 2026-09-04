package main

import (
	"bufio"
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Performance flags as defined for the TS_UD_CS_CORE performanceFlags field
// (MS-RDPBCGR, section 2.2.1.3.2). The .rdp "disable ..." keys are just a
// spelled-out form of these bits.
const (
	perfDisableWallpaper         = 0x00000001
	perfDisableFullWindowDrag    = 0x00000002
	perfDisableMenuAnimations    = 0x00000004
	perfDisableTheming           = 0x00000008
	perfDisableCursorShadow      = 0x00000020
	perfDisableCursorSettings    = 0x00000040
	perfEnableFontSmoothing      = 0x00000080
	perfEnableDesktopComposition = 0x00000100
)

// rdpEntry is one "name:type:value" line of a .rdp file.
type rdpEntry struct {
	Name  string
	Type  string // "i", "s" or "b"
	Value string
	Line  int
}

// readRDPFile decodes a .rdp file. mstsc writes UTF-16LE with a BOM; files
// written by hand or by other tools are usually UTF-8, so both are accepted.
func readRDPFile(path string) ([]rdpEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := decodeRDPText(raw)

	var entries []rdpEntry
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		// name:type:value  -- the value may itself contain colons.
		first := strings.Index(trimmed, ":")
		if first < 0 {
			logWarnf("%s:%d: ignoring malformed line %q", path, lineNo, trimmed)
			continue
		}
		second := strings.Index(trimmed[first+1:], ":")
		if second < 0 {
			logWarnf("%s:%d: ignoring malformed line %q", path, lineNo, trimmed)
			continue
		}
		second += first + 1
		entries = append(entries, rdpEntry{
			Name:  strings.ToLower(strings.TrimSpace(trimmed[:first])),
			Type:  strings.ToLower(strings.TrimSpace(trimmed[first+1 : second])),
			Value: trimmed[second+1:],
			Line:  lineNo,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func decodeRDPText(raw []byte) string {
	switch {
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE: // UTF-16LE BOM
		return decodeUTF16(raw[2:], binary.LittleEndian)
	case len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF: // UTF-16BE BOM
		return decodeUTF16(raw[2:], binary.BigEndian)
	case len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF: // UTF-8 BOM
		return string(raw[3:])
	}
	// Heuristic: mstsc omits the BOM in some versions; interleaved NUL bytes
	// in the first bytes mean UTF-16LE.
	if len(raw) >= 4 && raw[1] == 0 && raw[3] == 0 {
		return decodeUTF16(raw, binary.LittleEndian)
	}
	return string(raw)
}

func decodeUTF16(b []byte, order binary.ByteOrder) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = order.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

// applyRDPFile overlays a .rdp file onto cfg.
func applyRDPFile(cfg *Config, path string) error {
	entries, err := readRDPFile(path)
	if err != nil {
		return errf("reading %s: %w", path, err)
	}
	cfg.RDPFile = path

	var perf int
	perfSeen := false
	setPerf := func(bit int, on bool) {
		perfSeen = true
		if on {
			perf |= bit
		} else {
			perf &^= bit
		}
	}

	num := func(e rdpEntry) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(e.Value))
		if err != nil {
			logWarnf("%s:%d: %s: %q is not a number, ignored", path, e.Line, e.Name, e.Value)
			return 0, false
		}
		return n, true
	}
	flag := func(e rdpEntry) (bool, bool) {
		n, ok := num(e)
		return n != 0, ok
	}

	var screenMode int
	screenModeSeen := false

	for _, e := range entries {
		switch e.Name {
		case "full address", "alternate full address":
			if e.Name == "alternate full address" && cfg.Server != "" {
				continue
			}
			host, port, err := splitHostPort(e.Value)
			if err != nil {
				logWarnf("%s:%d: %v", path, e.Line, err)
				continue
			}
			cfg.Server = host
			if port != 0 {
				cfg.Port = port
			}
		case "server port":
			if n, ok := num(e); ok {
				cfg.Port = n
			}
		case "username":
			cfg.Username = e.Value
		case "domain":
			cfg.Domain = e.Value
		case "password 51":
			if pw, err := decryptRDPPassword(e.Value); err != nil {
				logWarnf("%s:%d: stored password could not be decrypted (%v); "+
					"it is protected for the user who saved the file", path, e.Line, err)
			} else {
				cfg.Password, cfg.PasswordSet = pw, true
				logDebugf("using the password stored in %s", path)
			}
		case "desktopwidth":
			if n, ok := num(e); ok {
				cfg.Width = n
			}
		case "desktopheight":
			if n, ok := num(e); ok {
				cfg.Height = n
			}
		case "screen mode id":
			if n, ok := num(e); ok {
				screenMode, screenModeSeen = n, true
			}
		case "use multimon":
			if b, ok := flag(e); ok {
				cfg.MultiMon = b
			}
		case "span monitors":
			if b, ok := flag(e); ok {
				cfg.Span = b
			}
		case "session bpp":
			if n, ok := num(e); ok {
				cfg.ColorDepth = n
			}
		case "smart sizing":
			if b, ok := flag(e); ok {
				cfg.SmartSizing = boolPtr(b)
			}
		case "desktopscalefactor":
			if n, ok := num(e); ok {
				if n < 100 || n > 500 {
					logWarnf("%s:%d: desktopscalefactor %d out of range, ignored", path, e.Line, n)
				} else {
					cfg.Scale = n
				}
			}
		case "devicescalefactor":
			if n, ok := num(e); ok {
				valid := false
				for _, v := range deviceScaleBuckets {
					valid = valid || v == n
				}
				if !valid {
					logWarnf("%s:%d: devicescalefactor %d is not one of %v, ignored",
						path, e.Line, n, deviceScaleBuckets)
				} else {
					cfg.DeviceScale = n
				}
			}
		case "dynamic resolution":
			if b, ok := flag(e); ok {
				cfg.DynamicResize = b
			}
		case "displayconnectionbar":
			if b, ok := flag(e); ok {
				cfg.ConnectionBar = boolPtr(b)
			}
		case "pinconnectionbar":
			if b, ok := flag(e); ok {
				cfg.PinConnBar = boolPtr(b)
			}
		case "compression":
			if b, ok := flag(e); ok {
				cfg.Compression = boolPtr(b)
			}
		case "audiomode":
			if n, ok := num(e); ok {
				cfg.AudioMode = intPtr(n)
			}
		case "audiocapturemode":
			if n, ok := num(e); ok {
				cfg.AudioCaptureMode = intPtr(n)
			}
		case "audioqualitymode":
			if n, ok := num(e); ok {
				cfg.AudioQuality = intPtr(n)
			}
		case "keyboardhook":
			if n, ok := num(e); ok {
				cfg.KeyboardHook = intPtr(n)
			}
		case "redirectclipboard":
			if b, ok := flag(e); ok {
				cfg.RedirectClipboard = boolPtr(b)
			}
		case "redirectprinters":
			if b, ok := flag(e); ok {
				cfg.RedirectPrinters = boolPtr(b)
			}
		case "redirectcomports":
			if b, ok := flag(e); ok {
				cfg.RedirectPorts = boolPtr(b)
			}
		case "redirectsmartcards":
			if b, ok := flag(e); ok {
				cfg.RedirectSmartCards = boolPtr(b)
			}
		case "redirectposdevices":
			if b, ok := flag(e); ok {
				cfg.RedirectPOS = boolPtr(b)
			}
		case "redirectdrives":
			if b, ok := flag(e); ok {
				cfg.RedirectDrives = boolPtr(b)
			}
		case "drivestoredirect":
			cfg.DrivesToRedirect = e.Value
			if strings.TrimSpace(e.Value) != "" {
				cfg.RedirectDrives = boolPtr(true)
			}
		case "authentication level":
			if n, ok := num(e); ok {
				cfg.AuthLevel = intPtr(n)
			}
		case "enablecredsspsupport":
			if b, ok := flag(e); ok {
				cfg.EnableCredSSP = boolPtr(b)
			}
		case "negotiate security layer":
			if b, ok := flag(e); ok {
				cfg.NegotiateLayer = boolPtr(b)
			}
		case "prompt for credentials", "prompt for credentials on client":
			if b, ok := flag(e); ok && b {
				cfg.PromptForCreds = true
			}
		case "administrative session", "connect to console":
			if b, ok := flag(e); ok {
				cfg.AdminSession = b
			}
		case "restricted admin mode":
			if b, ok := flag(e); ok {
				cfg.RestrictedAdmin = b
			}
		case "remotecredentialguard":
			if b, ok := flag(e); ok {
				cfg.RemoteGuard = b
			}
		case "autoreconnection enabled":
			if b, ok := flag(e); ok {
				cfg.AutoReconnect = boolPtr(b)
			}
		case "autoreconnect max retries":
			if n, ok := num(e); ok {
				cfg.MaxReconnect = intPtr(n)
			}
		case "networkautodetect":
			if b, ok := flag(e); ok {
				cfg.NetworkAutoDetect = boolPtr(b)
			}
		case "bandwidthautodetect":
			if b, ok := flag(e); ok {
				cfg.BandwidthDetect = boolPtr(b)
			}
		case "connection type":
			if n, ok := num(e); ok {
				cfg.NetworkType = intPtr(n)
			}
		case "alternate shell":
			cfg.StartProgram = e.Value
		case "shell working directory":
			cfg.WorkDir = e.Value
		case "loadbalanceinfo":
			cfg.LoadBalanceInfo = e.Value
		case "gatewayhostname":
			cfg.Gateway.Hostname = e.Value
		case "gatewayusagemethod":
			if n, ok := num(e); ok {
				cfg.Gateway.UsageMethod = intPtr(n)
			}
		case "gatewaycredentialssource":
			if n, ok := num(e); ok {
				cfg.Gateway.CredsSource = intPtr(n)
			}
		case "gatewayprofileusagemethod":
			if n, ok := num(e); ok {
				cfg.Gateway.ProfileUsage = intPtr(n)
			}
		case "gatewayusername":
			cfg.Gateway.Username = e.Value
		case "gatewaydomain":
			cfg.Gateway.Domain = e.Value
		case "promptcredentialonce":
			if b, ok := flag(e); ok {
				cfg.Gateway.CredsSharing = boolPtr(b)
			}
		case "performanceflags":
			if n, ok := num(e); ok {
				perf, perfSeen = n, true
			}
		case "disable wallpaper":
			if b, ok := flag(e); ok {
				setPerf(perfDisableWallpaper, b)
			}
		case "disable full window drag":
			if b, ok := flag(e); ok {
				setPerf(perfDisableFullWindowDrag, b)
			}
		case "disable menu anims":
			if b, ok := flag(e); ok {
				setPerf(perfDisableMenuAnimations, b)
			}
		case "disable themes":
			if b, ok := flag(e); ok {
				setPerf(perfDisableTheming, b)
			}
		case "disable cursor setting":
			if b, ok := flag(e); ok {
				setPerf(perfDisableCursorSettings, b)
			}
		case "allow desktop composition", "allow desktop com position":
			if b, ok := flag(e); ok {
				setPerf(perfEnableDesktopComposition, b)
			}
		case "allow font smoothing":
			if b, ok := flag(e); ok {
				setPerf(perfEnableFontSmoothing, b)
			}
		case "disable cursor shadow":
			if b, ok := flag(e); ok {
				setPerf(perfDisableCursorShadow, b)
			}
		case "winposstr", "bitmapcachepersistenable", "bitmapcachesize",
			"videoplaybackmode", "redirectdirectx", "usbdevicestoredirect",
			"camerastoredirect", "devicestoredirect", "selectedmonitors",
			"maximizetocurrentdisplays", "singlemoninwindowedmode",
			"enableworkspacereconnect",
			"workspaceid", "kdcproxyname", "rdgiskdcproxy", "public mode",
			"remoteapplicationmode", "remoteapplicationname",
			"remoteapplicationprogram", "remoteapplicationcmdline",
			"remoteapplicationicon", "remoteapplicationexpandcmdline",
			"remoteapplicationexpandworkingdir", "remoteapplicationfile",
			"disableconnectionsharing", "gatewaybrokeringtype",
			"use redirection server name", "targetisaadjoined",
			"enablerdsaadauth", "administrative session id":
			logDebugf("%s:%d: %s is not applied by %s", path, e.Line, e.Name, appName)
		default:
			logDebugf("%s:%d: unrecognised setting %q, ignored", path, e.Line, e.Name)
		}
	}

	if screenModeSeen {
		// screen mode id: 1 = windowed, 2 = full screen.
		cfg.FullScreen = screenMode == 2
	}
	if perfSeen {
		cfg.PerformanceFlags = intPtr(perf)
	}
	return nil
}

// looksLikeRDPFile reports whether an argument should be treated as a
// connection file rather than a switch.
//
// An argument that starts with "/" or "-" is normally a switch, but a path can
// legitimately start that way (a POSIX-style path, or a UNC path typed with
// forward slashes), so such an argument still counts as a connection file when
// it names one that exists.
func looksLikeRDPFile(arg string) bool {
	if arg == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(filepathExt(arg)), ".rdp") {
		return false
	}
	if !strings.HasPrefix(arg, "/") && !strings.HasPrefix(arg, "-") {
		return true
	}
	st, err := os.Stat(arg)
	return err == nil && !st.IsDir()
}

func filepathExt(p string) string {
	for i := len(p) - 1; i >= 0 && !isPathSep(p[i]); i-- {
		if p[i] == '.' {
			return p[i:]
		}
	}
	return ""
}

func isPathSep(c byte) bool { return c == '/' || c == '\\' }

// hexDecode parses the hex blob used by "password 51:b:".
func hexDecode(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
	if len(s)%2 != 0 {
		return nil, errf("odd length hex string")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, errf("invalid hex byte %q", s[i*2:i*2+2])
		}
		out[i] = byte(n)
	}
	return out, nil
}

// utf16BytesToString decodes a UTF-16LE byte buffer, stopping at the first NUL.
func utf16BytesToString(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}
