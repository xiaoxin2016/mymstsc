package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port int
		bad  bool
	}{
		{in: "srv01", host: "srv01"},
		{in: "srv01:3390", host: "srv01", port: 3390},
		{in: "10.0.0.5", host: "10.0.0.5"},
		{in: "10.0.0.5:13389", host: "10.0.0.5", port: 13389},
		{in: "fe80::1", host: "fe80::1"},
		{in: "[fe80::1]", host: "fe80::1"},
		{in: "[fe80::1]:3389", host: "fe80::1", port: 3389},
		{in: "host.example.com:0", bad: true},
		{in: "host:70000", bad: true},
		{in: "host:abc", bad: true},
		{in: "[fe80::1", bad: true},
		{in: "", bad: true},
	}
	for _, c := range cases {
		host, port, err := splitHostPort(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("splitHostPort(%q) = %q,%d; want an error", c.in, host, port)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitHostPort(%q): unexpected error %v", c.in, err)
			continue
		}
		if host != c.host || port != c.port {
			t.Errorf("splitHostPort(%q) = %q,%d; want %q,%d", c.in, host, port, c.host, c.port)
		}
	}
}

func TestSplitUser(t *testing.T) {
	if u, d := splitUser(`CORP\alice`); u != "alice" || d != "CORP" {
		t.Errorf(`splitUser("CORP\alice") = %q,%q`, u, d)
	}
	if u, d := splitUser("alice@corp.example"); u != "alice@corp.example" || d != "" {
		t.Errorf(`splitUser("alice@corp.example") = %q,%q`, u, d)
	}
	if u, d := splitUser("alice"); u != "alice" || d != "" {
		t.Errorf(`splitUser("alice") = %q,%q`, u, d)
	}
}

func TestParseArgsBasics(t *testing.T) {
	res, err := parseArgs([]string{"/v:rds01:3390", `/u:CORP\alice`, "/w:1600", "/h:900", "/f"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	cfg := res.cfg
	if cfg.Server != "rds01" || cfg.Port != 3390 {
		t.Errorf("server = %q:%d", cfg.Server, cfg.Port)
	}
	if cfg.Username != "alice" || cfg.Domain != "CORP" {
		t.Errorf("user = %q domain = %q", cfg.Username, cfg.Domain)
	}
	if cfg.Width != 1600 || cfg.Height != 900 || !cfg.FullScreen {
		t.Errorf("display = %dx%d full=%v", cfg.Width, cfg.Height, cfg.FullScreen)
	}
}

func TestParseArgsSeparateValueAndEquals(t *testing.T) {
	res, err := parseArgs([]string{"/v", "rds01", "-w=1280", "--height", "720"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if res.cfg.Server != "rds01" || res.cfg.Width != 1280 || res.cfg.Height != 720 {
		t.Errorf("got %q %dx%d", res.cfg.Server, res.cfg.Width, res.cfg.Height)
	}
}

func TestParseArgsRedirectionToggles(t *testing.T) {
	res, err := parseArgs([]string{"/v:x", "/drives", "/clipboard:0", "/printers:1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	cfg := res.cfg
	if cfg.RedirectDrives == nil || !*cfg.RedirectDrives {
		t.Error("/drives should enable drive redirection")
	}
	if cfg.RedirectClipboard == nil || *cfg.RedirectClipboard {
		t.Error("/clipboard:0 should disable clipboard redirection")
	}
	if cfg.RedirectPrinters == nil || !*cfg.RedirectPrinters {
		t.Error("/printers:1 should enable printer redirection")
	}
	if cfg.RedirectPorts != nil {
		t.Error("an option that was not given must stay unset")
	}
}

func TestParseArgsRejectsUnsupportedAndUnknown(t *testing.T) {
	for _, arg := range []string{"/edit", "/shadow:3", "/migrate", "/nonsense"} {
		if _, err := parseArgs([]string{"/v:x", arg}); err == nil {
			t.Errorf("parseArgs(%q) should fail", arg)
		}
	}
}

func TestParseOverride(t *testing.T) {
	o, err := parseOverride("advanced.RedirectDirectX=1")
	if err != nil {
		t.Fatalf("parseOverride: %v", err)
	}
	if o.Target != "advanced" || o.Name != "RedirectDirectX" {
		t.Fatalf("got target=%q name=%q", o.Target, o.Name)
	}
	if v, ok := o.typedValue().(int32); !ok || v != 1 {
		t.Errorf("typedValue = %#v; want int32(1)", o.typedValue())
	}

	o2, err := parseOverride("SmartSizing=true")
	if err != nil {
		t.Fatalf("parseOverride: %v", err)
	}
	if o2.Target != "" || o2.Name != "SmartSizing" {
		t.Fatalf("got target=%q name=%q", o2.Target, o2.Name)
	}
	if v, ok := o2.typedValue().(bool); !ok || !v {
		t.Errorf("typedValue = %#v; want true", o2.typedValue())
	}

	if o3, err := parseOverride("control.ConnectingText=s:123"); err != nil {
		t.Fatalf("parseOverride: %v", err)
	} else if v, ok := o3.typedValue().(string); !ok || v != "123" {
		t.Errorf(`"s:123" should stay the string "123", got %#v`, o3.typedValue())
	}

	for _, bad := range []string{"NoEquals", "=novalue", "bogus.Target.X=1"} {
		if _, err := parseOverride(bad); err == nil {
			t.Errorf("parseOverride(%q) should fail", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	cfg := newConfig()
	if err := cfg.Validate(); err == nil {
		t.Error("a config without a server must not validate")
	}
	cfg.Server = "rds01"
	cfg.ColorDepth = 17
	if err := cfg.Validate(); err == nil {
		t.Error("colour depth 17 must not validate")
	}
	cfg.ColorDepth = 32
	cfg.MultiMon, cfg.Span = true, true
	if err := cfg.Validate(); err == nil {
		t.Error("/span with /multimon must not validate")
	}
	cfg.Span = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !cfg.FullScreen {
		t.Error("/multimon should imply full screen")
	}
	if cfg.Title == "" {
		t.Error("a default window title should be filled in")
	}
}

const sampleRDP = `screen mode id:i:2
use multimon:i:0
desktopwidth:i:1920
desktopheight:i:1080
session bpp:i:32
full address:s:rds01.corp.example:3390
username:s:alice
domain:s:CORP
audiomode:i:0
redirectclipboard:i:1
redirectprinters:i:0
drivestoredirect:s:*
authentication level:i:2
enablecredsspsupport:i:1
administrative session:i:1
alternate shell:s:C:\Windows\System32\cmd.exe
shell working directory:s:C:\Temp
gatewayhostname:s:gw.corp.example
gatewayusagemethod:i:1
disable wallpaper:i:1
disable themes:i:1
allow font smoothing:i:1
autoreconnection enabled:i:1
some future setting:i:1
`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyRDPFile(t *testing.T) {
	p := writeTemp(t, "session.rdp", sampleRDP)
	cfg := newConfig()
	if err := applyRDPFile(cfg, p); err != nil {
		t.Fatalf("applyRDPFile: %v", err)
	}
	if cfg.Server != "rds01.corp.example" || cfg.Port != 3390 {
		t.Errorf("server = %q:%d", cfg.Server, cfg.Port)
	}
	if cfg.Username != "alice" || cfg.Domain != "CORP" {
		t.Errorf("user = %q domain = %q", cfg.Username, cfg.Domain)
	}
	if cfg.Width != 1920 || cfg.Height != 1080 || cfg.ColorDepth != 32 {
		t.Errorf("display = %dx%d bpp %d", cfg.Width, cfg.Height, cfg.ColorDepth)
	}
	if !cfg.FullScreen {
		t.Error("screen mode id 2 means full screen")
	}
	if !cfg.AdminSession {
		t.Error("administrative session was not picked up")
	}
	if cfg.StartProgram != `C:\Windows\System32\cmd.exe` || cfg.WorkDir != `C:\Temp` {
		t.Errorf("alternate shell = %q, workdir = %q", cfg.StartProgram, cfg.WorkDir)
	}
	if cfg.Gateway.Hostname != "gw.corp.example" ||
		cfg.Gateway.UsageMethod == nil || *cfg.Gateway.UsageMethod != 1 {
		t.Errorf("gateway = %+v", cfg.Gateway)
	}
	if cfg.RedirectClipboard == nil || !*cfg.RedirectClipboard {
		t.Error("redirectclipboard:i:1 was not applied")
	}
	if cfg.RedirectPrinters == nil || *cfg.RedirectPrinters {
		t.Error("redirectprinters:i:0 was not applied")
	}
	if cfg.RedirectDrives == nil || !*cfg.RedirectDrives {
		t.Error("drivestoredirect:s:* should enable drive redirection")
	}
	want := perfDisableWallpaper | perfDisableTheming | perfEnableFontSmoothing
	if cfg.PerformanceFlags == nil || *cfg.PerformanceFlags != want {
		t.Errorf("performance flags = %v; want %#x", cfg.PerformanceFlags, want)
	}
}

func TestSwitchesOverrideRDPFile(t *testing.T) {
	p := writeTemp(t, "session.rdp", sampleRDP)
	res, err := parseArgs([]string{p, "/v:other01", "/w:1280", "/h:800"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	cfg := res.cfg
	if cfg.Server != "other01" {
		t.Errorf("/v should override the file, got %q", cfg.Server)
	}
	if cfg.Width != 1280 || cfg.Height != 800 {
		t.Errorf("size = %dx%d", cfg.Width, cfg.Height)
	}
	// Settings the switches did not mention must survive from the file.
	if cfg.Username != "alice" || cfg.Gateway.Hostname != "gw.corp.example" {
		t.Errorf("file settings were lost: user=%q gw=%q", cfg.Username, cfg.Gateway.Hostname)
	}
	if cfg.RDPFile != p {
		t.Errorf("RDPFile = %q", cfg.RDPFile)
	}
}

func TestReadRDPFileUTF16(t *testing.T) {
	// mstsc writes .rdp files as UTF-16LE with a byte order mark.
	body := "full address:s:rds01\r\nusername:s:bob\r\n"
	u := utf16.Encode([]rune(body))
	raw := []byte{0xFF, 0xFE}
	for _, c := range u {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], c)
		raw = append(raw, b[0], b[1])
	}
	p := filepath.Join(t.TempDir(), "u16.rdp")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := newConfig()
	if err := applyRDPFile(cfg, p); err != nil {
		t.Fatalf("applyRDPFile: %v", err)
	}
	if cfg.Server != "rds01" || cfg.Username != "bob" {
		t.Errorf("server=%q user=%q", cfg.Server, cfg.Username)
	}
}

func TestRDPValuesMayContainColons(t *testing.T) {
	p := writeTemp(t, "x.rdp", "alternate shell:s:C:\\Program Files\\app.exe /arg:1\n")
	cfg := newConfig()
	if err := applyRDPFile(cfg, p); err != nil {
		t.Fatalf("applyRDPFile: %v", err)
	}
	if cfg.StartProgram != `C:\Program Files\app.exe /arg:1` {
		t.Errorf("alternate shell = %q", cfg.StartProgram)
	}
}

func TestLooksLikeRDPFile(t *testing.T) {
	for _, s := range []string{"a.rdp", `C:\path\to\my session.RDP`, "./x.Rdp"} {
		if !looksLikeRDPFile(s) {
			t.Errorf("looksLikeRDPFile(%q) = false", s)
		}
	}
	for _, s := range []string{"/v:host", "-f", "host.rdp.txt", ""} {
		if looksLikeRDPFile(s) {
			t.Errorf("looksLikeRDPFile(%q) = true", s)
		}
	}
}

func TestHexDecode(t *testing.T) {
	b, err := hexDecode("01ff A0")
	if err != nil {
		t.Fatalf("hexDecode: %v", err)
	}
	if len(b) != 3 || b[0] != 0x01 || b[1] != 0xFF || b[2] != 0xA0 {
		t.Errorf("hexDecode = % x", b)
	}
	if _, err := hexDecode("abc"); err == nil {
		t.Error("odd-length input should fail")
	}
	if _, err := hexDecode("zz"); err == nil {
		t.Error("non-hex input should fail")
	}
}

func TestVersionedNames(t *testing.T) {
	got := versionedNames("AdvancedSettings", 3)
	want := []string{"AdvancedSettings3", "AdvancedSettings2", "AdvancedSettings"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v; want %v", got, want)
		}
	}
}

func TestParseLogLevel(t *testing.T) {
	if l, err := parseLogLevel("debug"); err != nil || l != logDebug {
		t.Errorf("debug -> %v, %v", l, err)
	}
	if _, err := parseLogLevel("loud"); err == nil {
		t.Error("an unknown level should fail")
	}
}
