package main

// Go implementation of Bluetooth to USB HID proxy
// Author: Taneli Leppä <rosmo@rosmo.fi>
// Licensed under Apache License 2.0

import (
	hidproxy "github.com/rosmo/go-hidproxy"
	"flag"
	"fmt"
	log "github.com/sirupsen/logrus"
)

func main() {
	logLevelPtr := flag.String("loglevel", "info", "log level (panic, fatal, error, warn, info, debug, trace)")
	setupHid := flag.Bool("setuphid", true, "setup HID files on startup")
	setupMouse := flag.Bool("mouse", true, "setup mouse(s)")
	setupKeyboard := flag.Bool("keyboard", true, "setup keyboard(s)")
	monitorUdev := flag.Bool("monitor-udev", true, "monitor udev & BlueZ events for disconnects")
	adapterId := flag.String("bluez-adapter", "hci0", "BlueZ adapter (default hci0)")
	kbdRepeat := flag.Int("kbdrepeat", 62, "set keyboard repeat rate (default 62)")
	kbdDelay := flag.Int("kbddelay", 300, "set keyboard repeat delay in ms (default 300)")
	flag.Parse()

	logLevel, err := log.ParseLevel(*logLevelPtr)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Set log level: %v\n", logLevel)
	log.SetLevel(logLevel)

	hidproxy.Start(hidproxy.Config{
		SetupHid: *setupHid,
		SetupMouse: *setupMouse,
		SetupKeyboard: *setupKeyboard,
		MonitorUdev: *monitorUdev,
		AdapterId: *adapterId,
		KbdRepeat: *kbdRepeat,
		KbdDelay: *kbdDelay,
		LogLevel: logLevel,
	})
}
