package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const appName = "mymstsc"

const usageText = `%[1]s - a portable Remote Desktop client

%[1]s hosts the Remote Desktop ActiveX control that ships with Windows
(%%SystemRoot%%\System32\mstscax.dll), so it needs neither mstsc.exe nor the
matching mstsc.exe.mui and can be copied to a machine as a single file.

Usage:
  %[1]s [<file>.rdp] [options]

Connection:
  /v:<server[:port]>     server to connect to
  /port:<n>              server port when it is not part of /v
  /u:<user>              user name ("DOMAIN\user" and "user@domain" both work)
  /d:<domain>            logon domain
  /p:<password>          password; "/p:-" reads it from the console instead.
                         Prefer /p:- or %[2]s, because a command line
                         is visible to every process on the machine.
  /prompt                always ask Windows for credentials

Display:
  /f                     full screen
  /w:<n> /h:<n>          session width and height in pixels
  /span                  span the session across all monitors
  /multimon              use true multiple-monitor mode
  /bpp:<n>               colour depth: 8, 15, 16, 24 or 32
  /smartsizing[:0|1]     scale the session to the window
  /scale:auto|<percent>  desktop scale factor; "auto" (default) follows the DPI
                         of the monitor the window is on, so a session on a
                         high-DPI screen is as readable as a local one
  /devicescale:<percent> device scale factor: 100, 140 or 180. Derived from
                         /scale when not given
  /conbar:0|1            show the connection bar in full screen (default 1)
  /noresize              do not resize the remote desktop with the window
  /title:<text>          window title

Session:
  /admin                 connect to the administrative session
  /public                public mode: do not persist credentials or caches
  /restrictedAdmin       Restricted Admin mode
  /remoteGuard           Remote Credential Guard
  /shell:<program>       start a program instead of the desktop
  /workdir:<path>        working directory for /shell

Redirection (each takes an optional :0 or :1, default 1):
  /drives  /clipboard  /printers  /ports  /smartcards
  /audio:<n>             0 play here, 1 play on server, 2 do not play

Gateway:
  /g:<host>              RD Gateway host name
  /gu:<user>             gateway user name
  /gd:<domain>           gateway domain
  /gp:<password>         gateway password ("-" reads it from the console)

Diagnostics and escape hatches:
  /set:<path>=<value>    write any control property directly, for example
                         /set:advanced.RedirectDirectX=1
                         paths: control.X advanced.X secured.X transport.X
                                nonscriptable.X
  /coclass:<name>        force a control class, e.g. MsRdpClient9NotSafeForScripting
  /list-classes          list the control classes this Windows build offers
  /log:<level>           quiet, error, warn, info (default) or debug
  /timestamps            prefix log lines with the time
  /? /help               this text
  /version               version information

Environment:
  %[2]s   password, used when /p is not given
  %[3]s   gateway password

Exit status: 0 on a normal disconnect, 1 on error, 2 on a failed logon.
`

const (
	envPassword        = "MYMSTSC_PASSWORD"
	envGatewayPassword = "MYMSTSC_GATEWAY_PASSWORD"
)

// unsupportedSwitches are mstsc options this program deliberately does not
// implement, listed so the user gets a clear message instead of silence.
var unsupportedSwitches = map[string]string{
	"edit":            "editing .rdp files is out of scope; edit the file directly",
	"shadow":          "session shadowing is not exposed by the client control",
	"control":         "session shadowing is not exposed by the client control",
	"noconsentprompt": "session shadowing is not exposed by the client control",
	"migrate":         "migrating legacy Client Connection Manager files is not supported",
	"l":               "listing sessions is not exposed by the client control",
}

type parseResult struct {
	cfg      *Config
	showHelp bool
	showVer  bool
}

// parseArgs builds a Config from mstsc-style arguments. A .rdp file is read
// first so that individual switches override it, which is how mstsc behaves.
func parseArgs(args []string) (*parseResult, error) {
	cfg := newConfig()
	res := &parseResult{cfg: cfg}

	// Pass 1: locate the connection file, so switches can override it.
	var rest []string
	for _, a := range args {
		if looksLikeRDPFile(a) && cfg.RDPFile == "" {
			if err := applyRDPFile(cfg, a); err != nil {
				return nil, err
			}
			continue
		}
		rest = append(rest, a)
	}

	pwFromCLI := false
	gpwFromCLI := false

	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "/") && !strings.HasPrefix(arg, "-") {
			return nil, errf("unexpected argument %q (a connection file must end in .rdp)", arg)
		}
		body := strings.TrimLeft(arg, "-/")
		name, value, hasValue := splitSwitch(body)
		lname := strings.ToLower(name)

		if reason, bad := unsupportedSwitches[lname]; bad {
			return nil, errf("/%s is not supported: %s", name, reason)
		}

		// Allow "/w 1920" as well as "/w:1920".
		needValue := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "/") {
				i++
				return rest[i], nil
			}
			return "", errf("/%s requires a value", name)
		}
		intValue := func() (int, error) {
			v, err := needValue()
			if err != nil {
				return 0, err
			}
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return 0, errf("/%s: %q is not a number", name, v)
			}
			return n, nil
		}
		// Redirection-style flags: present means on, ":0" means off.
		onOff := func() (bool, error) {
			if !hasValue {
				return true, nil
			}
			return parseOnOff(value, name)
		}

		var err error
		switch lname {
		case "?", "help":
			res.showHelp = true
		case "version":
			res.showVer = true

		case "v":
			var v string
			if v, err = needValue(); err == nil {
				var host string
				var port int
				if host, port, err = splitHostPort(v); err == nil {
					cfg.Server = host
					if port != 0 {
						cfg.Port = port
					}
				}
			}
		case "port":
			cfg.Port, err = intValue()
		case "u", "user", "username":
			var v string
			if v, err = needValue(); err == nil {
				user, domain := splitUser(v)
				cfg.Username = user
				if domain != "" {
					cfg.Domain = domain
				}
			}
		case "d", "domain":
			cfg.Domain, err = needValue()
		case "p", "password":
			var v string
			if v, err = needValue(); err == nil {
				if v == "-" {
					cfg.ReadPwStdin = true
				} else {
					cfg.Password, cfg.PasswordSet = v, true
					pwFromCLI = true
				}
			}
		case "prompt":
			cfg.PromptForCreds = true

		case "f", "fullscreen":
			cfg.FullScreen = true
		case "w", "width":
			cfg.Width, err = intValue()
		case "h", "height":
			cfg.Height, err = intValue()
		case "span":
			cfg.Span = true
		case "multimon":
			cfg.MultiMon = true
		case "bpp", "colordepth":
			cfg.ColorDepth, err = intValue()
		case "scale":
			var v string
			if v, err = needValue(); err == nil {
				cfg.Scale, err = parseScale(v)
			}
		case "devicescale":
			var v string
			if v, err = needValue(); err == nil {
				cfg.DeviceScale, err = parseDeviceScale(v)
			}
		case "smartsizing":
			var b bool
			if b, err = onOff(); err == nil {
				cfg.SmartSizing = boolPtr(b)
			}
		case "conbar", "connectionbar":
			var b bool
			if b, err = onOff(); err == nil {
				cfg.ConnectionBar = boolPtr(b)
			}
		case "noresize":
			cfg.DynamicResize = false
		case "title":
			cfg.Title, err = needValue()

		case "admin", "console":
			cfg.AdminSession = true
		case "public":
			cfg.Public = true
		case "restrictedadmin":
			cfg.RestrictedAdmin = true
		case "remoteguard":
			cfg.RemoteGuard = true
		case "shell":
			cfg.StartProgram, err = needValue()
		case "workdir":
			cfg.WorkDir, err = needValue()

		case "drives":
			var b bool
			if b, err = onOff(); err == nil {
				cfg.RedirectDrives = boolPtr(b)
			}
		case "clipboard":
			var b bool
			if b, err = onOff(); err == nil {
				cfg.RedirectClipboard = boolPtr(b)
			}
		case "printers":
			var b bool
			if b, err = onOff(); err == nil {
				cfg.RedirectPrinters = boolPtr(b)
			}
		case "ports":
			var b bool
			if b, err = onOff(); err == nil {
				cfg.RedirectPorts = boolPtr(b)
			}
		case "smartcards":
			var b bool
			if b, err = onOff(); err == nil {
				cfg.RedirectSmartCards = boolPtr(b)
			}
		case "audio":
			var n int
			if n, err = intValue(); err == nil {
				cfg.AudioMode = intPtr(n)
			}

		case "g", "gateway":
			cfg.Gateway.Hostname, err = needValue()
		case "gu":
			var v string
			if v, err = needValue(); err == nil {
				user, domain := splitUser(v)
				cfg.Gateway.Username = user
				if domain != "" {
					cfg.Gateway.Domain = domain
				}
			}
		case "gd":
			cfg.Gateway.Domain, err = needValue()
		case "gp":
			var v string
			if v, err = needValue(); err == nil {
				if v == "-" {
					var pw string
					if pw, err = readSecret("Gateway password: "); err == nil {
						cfg.Gateway.Password, cfg.Gateway.PasswordSet = pw, true
					}
				} else {
					cfg.Gateway.Password, cfg.Gateway.PasswordSet = v, true
					gpwFromCLI = true
				}
			}

		case "set":
			var v string
			if v, err = needValue(); err == nil {
				var o propOverride
				if o, err = parseOverride(v); err == nil {
					cfg.Overrides = append(cfg.Overrides, o)
				}
			}
		case "coclass":
			cfg.CoclassName, err = needValue()
		case "list-classes", "listclasses":
			cfg.ListClasses = true
		case "log":
			var v string
			if v, err = needValue(); err == nil {
				cfg.LogLevel, err = parseLogLevel(v)
			}
		case "timestamps":
			cfg.Timestamps = true

		default:
			return nil, errf("unknown option %q (try /?)", arg)
		}
		if err != nil {
			return nil, err
		}
	}

	if res.showHelp || res.showVer {
		return res, nil
	}

	// Credentials that did not come from the command line.
	if !cfg.PasswordSet && !cfg.ReadPwStdin {
		if v, ok := os.LookupEnv(envPassword); ok {
			cfg.Password, cfg.PasswordSet = v, true
		}
	}
	if !cfg.Gateway.PasswordSet {
		if v, ok := os.LookupEnv(envGatewayPassword); ok {
			cfg.Gateway.Password, cfg.Gateway.PasswordSet = v, true
		}
	}
	if cfg.ReadPwStdin {
		pw, err := readSecret("Password: ")
		if err != nil {
			return nil, err
		}
		cfg.Password, cfg.PasswordSet = pw, true
	}
	if pwFromCLI {
		logWarnf("a password passed on the command line is visible to other "+
			"processes; consider /p:- or the %s environment variable", envPassword)
	}
	if gpwFromCLI {
		logWarnf("a gateway password passed on the command line is visible to "+
			"other processes; consider /gp:- or the %s environment variable", envGatewayPassword)
	}
	return res, nil
}

// splitSwitch splits "name:value" or "name=value".
func splitSwitch(body string) (name, value string, ok bool) {
	if i := strings.IndexAny(body, ":="); i >= 0 {
		return body[:i], body[i+1:], true
	}
	return body, "", false
}

func parseOnOff(v, name string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "yes", "true":
		return true, nil
	case "0", "off", "no", "false":
		return false, nil
	}
	return false, errf("/%s: expected 0 or 1, got %q", name, v)
}

// splitUser accepts "DOMAIN\user" and "user@domain".
func splitUser(v string) (user, domain string) {
	if i := strings.Index(v, `\`); i >= 0 {
		return v[i+1:], v[:i]
	}
	// user@domain is a UPN; the control handles it as a whole, so keep it.
	return v, ""
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, usageText, appName, envPassword, envGatewayPassword)
}
