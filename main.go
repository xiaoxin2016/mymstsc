//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
)

// version is overridden at build time with
//
//	-ldflags "-X main.version=<tag>"
var version = "dev"

func main() {
	// The RDP control is an apartment-threaded COM object: it must be created,
	// pumped and destroyed on one and the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	code, err := run(os.Args[1:])
	if err != nil {
		reportFatal(err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	res, err := parseArgs(args)
	if err != nil {
		return 1, err
	}
	if res.showHelp {
		printUsage(os.Stdout)
		return 0, nil
	}
	if res.showVer {
		fmt.Printf("%s %s (%s/%s, %s)\n",
			appName, version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return 0, nil
	}

	cfg := res.cfg
	setLogLevel(cfg.LogLevel)
	logStamp = cfg.Timestamps

	if cfg.ListClasses {
		return listClasses()
	}
	if err := cfg.Validate(); err != nil {
		return 1, err
	}

	app := &App{cfg: cfg}
	theApp = app
	return app.Run()
}

// listClasses reports what the Remote Desktop control on this machine offers,
// which is the quickest way to see what a given Windows build supports.
func listClasses() (int, error) {
	path := mstscaxPath()
	tl, err := loadTypeLib(path)
	if err != nil {
		return 1, err
	}
	defer tl.Close()

	fmt.Printf("Remote Desktop client control: %s\n\n", path)
	fmt.Println("Coclasses, best candidate first:")
	for i, c := range rdpCoclasses(tl) {
		mark := "  "
		if i == 0 {
			mark = "->"
		}
		fmt.Printf("%s %-40s %s\n", mark, c.Type.Name, c.Type.GUID)
	}

	ns := nonScriptableInterfaces(tl)
	if len(ns) > 0 {
		fmt.Println("\nNon-scriptable interfaces, newest first:")
		for _, t := range ns {
			fmt.Printf("   %-40s %s\n", t.Name, t.GUID)
		}
	}
	return 0, nil
}
