package main

// Go implementation of Bluetooth to USB HID proxy
// Author: Taneli Leppä <rosmo@rosmo.fi>
// Licensed under Apache License 2.0

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	evdev "github.com/gvalkov/golang-evdev"
	udev "github.com/jochenvg/go-udev"
	"github.com/loov/hrtime"
	"github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez/profile/adapter"
	log "github.com/sirupsen/logrus"
	orderedmap "github.com/wk8/go-ordered-map"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type InputDevice struct {
	Device string
	Name   string
}

type InputMessage struct {
	Message   []byte
	Timestamp time.Duration
}

var Scancodes = map[uint16]uint16{
	2:   30, // 1
	3:   31, // 2
	4:   32, // 3
	5:   33, // 4
	6:   34, // 5
	7:   35, // 6
	8:   36, // 7
	9:   37, // 8
	10:  38, // 9
	11:  39, // 0
	57:  44, // space
	14:  42, // bkspc
	28:  40, // enter
	1:   41, // ESC
	106: 79, // RIGHT
	105: 80, // LEFT
	108: 81, // DOWN
	103: 82, // UP
	59:  58, // F1
	60:  59, // F2
	61:  60, // F3
	62:  61, // F4
	63:  62, // F5
	64:  63, // F6
	65:  64, // F7
	66:  65, // F8
	67:  66, // F9
	68:  67, // F10
	69:  68, // F11
	70:  69, // F12
	12:  45, // -
	13:  46, // =
	15:  43, // TAB
	26:  47, // {
	27:  48, // ]
	39:  51, // :
	40:  52, // "
	51:  54, // <
	52:  55, // >
	53:  56, // ?
	41:  50, // //
	43:  49, // \
	30:  4,  // a
	48:  5,  // b
	46:  6,  // c
	32:  7,  // d
	18:  8,  // e
	33:  9,  // f
	34:  10, // g
	35:  11, // h
	23:  12, // i
	36:  13, // j
	37:  14, // k
	38:  15, // l
	50:  16, // m
	49:  17, // n
	24:  18, // o
	25:  19, // p
	16:  20, // q
	19:  21, // r
	31:  22, // s
	20:  23, // t
	22:  24, // u
	47:  25, // v
	17:  26, // w
	45:  27, // x
	21:  28, // y
	44:  29, // z
	86:  49, // | & \
	104: 75, // PgUp
	109: 78, // PgDn
	102: 74, // Home
	107: 77, // End
	110: 73, // Insert
	119: 72, // Pause
	//70: 71, // ScrLk
	99:  70,  // PrtSc
	87:  68,  // F11
	88:  69,  // F12
	113: 127, // Mute
	114: 129, // VolDn
	115: 128, // VolUp
	58:  57,  // CapsLock (non-locking)
	158: 122, // "Undo" (Thinkpad special key)
	159: 121, // "Again" (Thinkpad special key)
	29:  224, // Left-Ctrl
	125: 227, // Left-Cmd
	42:  225, // Left-Shift
	56:  226, // Left-Alt
	100: 230, // AltGr (Right-Alt)
	127: 231, // Right-Cmd
	97:  228, // Right-Ctrl
	54:  229, // Right-Shift
}

const (
	RIGHT_META    = 1 << 7
	RIGHT_ALT     = 1 << 6
	RIGHT_SHIFT   = 1 << 5
	RIGHT_CONTROL = 1 << 4
	LEFT_META     = 1 << 3
	LEFT_ALT      = 1 << 2
	LEFT_SHIFT    = 1 << 1
	LEFT_CONTROL  = 1 << 0

	BUTTON_LEFT   = 1 << 0
	BUTTON_RIGHT  = 1 << 1
	BUTTON_MIDDLE = 1 << 2
)

func SetupUSBGadget() {
	var paths = []string{
		"/sys/kernel/config/usb_gadget/piproxy",
		"/sys/kernel/config/usb_gadget/piproxy/strings/0x409",
		"/sys/kernel/config/usb_gadget/piproxy/configs/c.1/strings/0x409",
		"/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb0",
		"/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb1",
	}
	filesStr := orderedmap.New()
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/idVendor", "0x1d6b")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/idProduct", "0x0104")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/bcdDevice", "0x0100")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/bcdUSB", "0x0200")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/strings/0x409/serialnumber", "fedcba9876543210")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/strings/0x409/manufacturer", "Raspberry Pi")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/strings/0x409/product", "pizero keyboard Device")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/configs/c.1/strings/0x409/configuration", "Config 1: ECM network")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/configs/c.1/MaxPower", "250")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb0/protocol", "1")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb0/subclass", "1")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb0/report_length", "8")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb1/protocol", "2")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb1/subclass", "1")
	filesStr.Set("/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb1/report_length", "4")
	var filesBytes = map[string][]byte{
		"/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb0/report_desc": []byte{0x05, 0x01, 0x09, 0x06, 0xa1, 0x01, 0x05, 0x07, 0x19, 0xe0, 0x29, 0xe7, 0x15, 0x00, 0x25, 0x01, 0x75, 0x01, 0x95, 0x08, 0x81, 0x02, 0x95, 0x01, 0x75, 0x08, 0x81, 0x03, 0x95, 0x05, 0x75, 0x01, 0x05, 0x08, 0x19, 0x01, 0x29, 0x05, 0x91, 0x02, 0x95, 0x01, 0x75, 0x03, 0x91, 0x03, 0x95, 0x06, 0x75, 0x08, 0x15, 0x00, 0x25, 0x65, 0x05, 0x07, 0x19, 0x00, 0x29, 0x65, 0x81, 0x00, 0xc0},
		"/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb1/report_desc": []byte{0x05, 0x01, 0x09, 0x02, 0xa1, 0x01, 0x09, 0x01, 0xa1, 0x00, 0x05, 0x09, 0x19, 0x01, 0x29, 0x05, 0x15, 0x00, 0x25, 0x01, 0x95, 0x05, 0x75, 0x01, 0x81, 0x02, 0x95, 0x01, 0x75, 0x03, 0x81, 0x01, 0x05, 0x01, 0x09, 0x30, 0x09, 0x31, 0x09, 0x38, 0x15, 0x81, 0x25, 0x7f, 0x75, 0x08, 0x95, 0x03, 0x81, 0x06, 0xc0, 0xc0},
	}
	var symlinks = map[string]string{
		"/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb0": "/sys/kernel/config/usb_gadget/piproxy/configs/c.1/hid.usb0",
		"/sys/kernel/config/usb_gadget/piproxy/functions/hid.usb1": "/sys/kernel/config/usb_gadget/piproxy/configs/c.1/hid.usb1",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			log.Debugf("Creating directory: %s", path)
			err := os.MkdirAll(path, os.ModeDir)
			if err != nil {
				log.Fatalf("Failed to create directory path: %s", path)
			}
		}
	}

	for pair := filesStr.Oldest(); pair != nil; pair = pair.Next() {
		content, err := ioutil.ReadFile(pair.Key.(string))
		if err == nil {
			if bytes.Compare(content[0:len(content)-1], []byte(pair.Value.(string))) == 0 {
				continue
			}
		}

		log.Debugf("Writing file: %s", pair.Key.(string))
		err = ioutil.WriteFile(pair.Key.(string), []byte(pair.Value.(string)), os.FileMode(0644))
		if err != nil {
			log.Warnf("Failed to write file: %s (maybe already set up)", pair.Key.(string))
		}
	}

	for file, contents := range filesBytes {
		content, err := ioutil.ReadFile(file)
		if err == nil {
			if bytes.Compare(content, contents) == 0 {
				continue
			}
		}
		log.Debugf("Writing file: %s", file)
		err = ioutil.WriteFile(file, contents, os.FileMode(0644))
		if err != nil {
			log.Warnf("Failed to create file: %s (maybe already set up)", file)
		}
	}

	for source, target := range symlinks {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			log.Debugf("Creating symlink from %s to: %s", source, target)
			err := os.Symlink(source, target)
			if err != nil {
				log.Fatalf("Failed to create symlink %s -> %s", source, target)
			}
		}
	}

	time.Sleep(1000 * time.Millisecond)

	matches, err := filepath.Glob("/sys/class/udc/*")
	if err != nil {
		log.Fatalf("Failed to list files in /sys/class/udc: %s", err.Error())
	}
	var udcFile string = "/sys/kernel/config/usb_gadget/piproxy/UDC"
	var udc string = ""
	for _, match := range matches {
		udc = udc + filepath.Base(match) + " "
	}
	content, err := ioutil.ReadFile(udcFile)
	if err == nil {
		if bytes.Compare(content[0:len(content)-1], []byte(strings.TrimSpace(udc))) != 0 {
			err = ioutil.WriteFile(udcFile, []byte(strings.TrimSpace(udc)), os.FileMode(0644))
			if err != nil {
				log.Warnf("Failed to create file %s: %s: (%s)", udcFile, udc, err.Error())
			}
		}
	}
	// Give it a second to settle
	time.Sleep(1000 * time.Millisecond)
}

func HandleKeyboard(output chan<- error, input chan<- InputMessage, close <-chan bool, rate uint, delay uint, dev evdev.InputDevice) error {
	keysDown := make([]uint16, 0)
	err := dev.Grab()
	if err != nil {
		log.Fatal(err)
		output <- err
		return err
	}
	defer dev.Release()

	log.Infof("Grabbed keyboard-like device: %s (%s)", dev.Name, dev.Fn)
	syscall.SetNonblock(int(dev.File.Fd()), true)

	log.Infof("Setting repeat rate to %d, delay %d for %s (%s)", rate, delay, dev.Name, dev.Fn)
	dev.SetRepeatRate(rate, delay)

	loop := 0
	for {
		err = dev.File.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		if err != nil {
			log.Fatal(err)
			output <- err
			return err
		}

		event, err := dev.ReadOne()
		if err != nil && strings.Contains(err.Error(), "i/o timeout") {
			continue
		}
		if err != nil {
			log.Fatal(err)
			output <- err
			return err
		}
		log.Debugf("Keyboard input event: type=%d, code=%d, value=%d", event.Type, event.Code, event.Value)
		if event.Type == evdev.EV_KEY {
			keyEvent := evdev.NewKeyEvent(event)
			log.Debugf("Key event: scancode=%d, keycode=%d, state=%d", keyEvent.Scancode, keyEvent.Keycode, keyEvent.State)
			if keyCode, ok := Scancodes[keyEvent.Scancode]; ok {
				if keyEvent.State == 1 { // Key down
					keyIsDown := false
					for _, k := range keysDown {
						if k == keyCode {
							keyIsDown = true
						}
					}
					if !keyIsDown {
						keysDown = append(keysDown, keyCode)
					}
				}
				if keyEvent.State == 0 { // Key up
					newKeysDown := make([]uint16, 0)
					for _, k := range keysDown {
						if k != keyCode {
							newKeysDown = append(newKeysDown, k)
						}
					}
					keysDown = newKeysDown
				}

				var modifiers uint8 = 0
				keysToSend := make([]uint8, 0)
				for _, k := range keysDown {
					switch {
					case k == 224: // Left-Ctrl
						modifiers |= LEFT_CONTROL
					case k == 227: // Left-Cmd
						modifiers |= LEFT_META
					case k == 225: // Left-Shift
						modifiers |= LEFT_SHIFT
					case k == 226: // Left-Alt
						modifiers |= LEFT_ALT
					case k == 228: // Right-Ctrl
						modifiers |= RIGHT_CONTROL
					case k == 231: // Right-Cmd
						modifiers |= RIGHT_META
					case k == 229: // Right-Shift
						modifiers |= RIGHT_SHIFT
					case k == 230: // Right-Alt
						modifiers |= RIGHT_ALT
					default:
						keysToSend = append(keysToSend, uint8(k))
					}
				}
				keysToSend = append([]uint8{modifiers, 0}, keysToSend...)
				if len(keysToSend) < 8 {
					for i := len(keysToSend); i < 8; i++ {
						keysToSend = append(keysToSend, uint8(0))
					}
				}
				input <- InputMessage{
					Timestamp: hrtime.Now(),
					Message: keysToSend,
				}

				log.Debugf("Key status (scancode %d, keycode %d): %v\n", keyEvent.Scancode, keyCode, keysToSend)
			} else {
				log.Warnf("Unknown scancode: %d\n", keyEvent.Scancode)
			}
		}
		loop += 1
		if loop > 3 {
			select {
			case _ = <-close:
				log.Infof("Stopping processing keyboard input from: %s (%s)", dev.Name, dev.Fn)
				output <- nil
				return nil
			default:
			}
			loop = 0
		}
	}

	output <- nil
	return nil
}

func HandleMouse(output chan<- error, input chan<- InputMessage, close <-chan bool, dev evdev.InputDevice) error {
	err := dev.Grab()
	if err != nil {
		log.Fatal(err)
		output <- err
		return err
	}
	defer dev.Release()

	log.Infof("Grabbed mouse-like device: %s (%s)", dev.Name, dev.Fn)
	syscall.SetNonblock(int(dev.File.Fd()), true)

	loop := 0
	var buttons uint8 = 0x0
	for {
		err = dev.File.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		if err != nil {
			log.Fatal(err)
			output <- err
			return err
		}

		event, err := dev.ReadOne()
		if err != nil && strings.Contains(err.Error(), "i/o timeout") {
			continue
		}
		if err != nil {
			log.Fatal(err)
			output <- err
			return err
		}
		log.Debugf("Mouse input event: type=%d, code=%d, value=%d", event.Type, event.Code, event.Value)
		var buttonOp bool = false
		if event.Type == evdev.EV_KEY {
			if event.Code == 272 {
				if event.Value > 0 {
					buttons |= BUTTON_LEFT
				} else {
					buttons &= ^uint8(BUTTON_LEFT)
				}
				buttonOp = true
			}
			if event.Code == 273 {
				if event.Value > 0 {
					buttons |= BUTTON_RIGHT
				} else {
					buttons &= ^uint8(BUTTON_RIGHT)
				}
				buttonOp = true
			}
			if event.Code == 274 {
				if event.Value > 0 {
					buttons |= BUTTON_MIDDLE
				} else {
					buttons &= ^uint8(BUTTON_MIDDLE)
				}
				buttonOp = true
			}
		}
		if event.Type == evdev.EV_REL || buttonOp {
			mouseToSend := make([]uint8, 0)
			mouseToSend = append(mouseToSend, buttons)
			if event.Type == evdev.EV_REL {
				if event.Code == 0 {
					mouseToSend = append(mouseToSend, uint8(event.Value))
					mouseToSend = append(mouseToSend, 0x00)
					mouseToSend = append(mouseToSend, 0x00)
				}
				if event.Code == 1 {
					mouseToSend = append(mouseToSend, 0x00)
					mouseToSend = append(mouseToSend, uint8(event.Value))
					mouseToSend = append(mouseToSend, 0x00)
				}
				if event.Code == 11 {
					mouseToSend = append(mouseToSend, 0x00)
					mouseToSend = append(mouseToSend, 0x00)
					mouseToSend = append(mouseToSend, uint8(event.Value))
				}
			} else {
				mouseToSend = append(mouseToSend, 0x00)
				mouseToSend = append(mouseToSend, 0x00)
				mouseToSend = append(mouseToSend, 0x00)
			}
			input <- InputMessage{
					Timestamp: hrtime.Now(),
					Message: mouseToSend,
				}
		}
		loop += 1
		if loop > 3 {
			select {
			case _ = <-close:
				log.Infof("Stopping processing mouse input from: %s (%s)", dev.Name, dev.Fn)
				output <- nil
				return nil
			default:
			}
			loop = 0
		}
	}

	output <- nil
	return nil

}

func SendKeyboardReports(input <-chan InputMessage) error {
	log.Info("Opening keyboard /dev/hidg0 for writing...")
	file, err := os.OpenFile("/dev/hidg0", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Warn("Error opening /dev/hidg0, are you running as root?")
		log.Fatal(err)
		return err
	}
	defer file.Close()

	var avg, min, max, loop int64 = 0, 0, 0, 0
	for {
		msg := <-input
		bytesWritten, err := file.Write(msg.Message)
		if err != nil {
			log.Fatal(err)
			return err
		} 
		latency := hrtime.Since(msg.Timestamp).Nanoseconds()
		if latency < min {
			min = latency
		}
		if latency > max {
			max = latency
		}
		avg = (avg + latency) / 2
		loop += 1
		if loop > 50 {
			log.Debugf("Latency: now=%d, avg=%d, min=%d, max=%d μs", latency/1000, avg/1000, min/1000, max/1000)
			loop = 0
		}

		log.Debugf("Wrote %d bytes to /dev/hidg0 (%v)", bytesWritten, msg)
	}
	return nil
}

func SendMouseReports(input <-chan InputMessage) error {
	log.Info("Opening keyboard /dev/hidg1 for writing...")
	file, err := os.OpenFile("/dev/hidg1", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Warn("Error opening /dev/hidg1, are you running as root?")
		log.Fatal(err)
		return err
	}
	defer file.Close()

	var avg, min, max, loop int64 = 0, 0, 0, 0
	for {
		msg := <-input
		bytesWritten, err := file.Write(msg.Message)
		if err != nil {
			log.Fatal(err)
			return err
		}
		log.Debugf("Wrote %d bytes to /dev/hidg1 (%v)", bytesWritten, msg)
		latency := hrtime.Since(msg.Timestamp).Nanoseconds()
		if latency < min {
			min = latency
		}
		if latency > max {
			max = latency
		}
		avg = (avg + latency) / 2
		loop += 1
		if loop > 100 {
			log.Debugf("Latency: now=%d, avg=%d, min=%d, max=%d μs", latency/1000, avg/1000, min/1000, max/1000)
			loop = 0
		}
	}
	return nil
}

func GetDisconnectedDevices(adapterId string) ([]string, error) {
	log.Debugf("Getting adapter: %s", adapterId)
	a, err := adapter.GetAdapter(adapterId)
	if err != nil {
		return nil, err
	}

	log.Debugf("Getting devices from adapter: %s", adapterId)
	devices, err := a.GetDevices()
	if err != nil {
		return nil, err
	}

	disconnected := make([]string, 0)
	connected := make([]string, 0)
	for _, dev := range devices {
		address, err := dev.GetAddress()
		if err != nil {
			continue
		}
		name, err := dev.GetName()
		if err != nil {
			name = "?"
		}

		log.Infof("Checking if device %s (%s) is connected...", name, address)
		deviceConnected, err := dev.GetConnected()
		if err == nil {
			if !deviceConnected {
				log.Infof("Device %s is disconnected.", name)
				disconnected = append(disconnected, name)
			} else {
				log.Infof("Device %s is still connected.", name)
				connected = append(connected, name)
			}
		}
	}
	results := make([]string, 0)
	for _, dname := range disconnected {
		ok := true
		for _, cname := range connected {
			if cname == dname {
				ok = false
				break
			}
		}
		if ok {
			inResults := false
			for _, rname := range results {
				if rname == dname {
					inResults = true
				}
			}
			if !inResults {
				results = append(results, dname)
			}
		}
	}
	return results, nil
}

func main() {
	var wg sync.WaitGroup
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

	if *setupHid {
		log.Info("Setting up HID files...")
		SetupUSBGadget()
	}

	keyboardInput := make(chan InputMessage, 10)
	mouseInput := make(chan InputMessage, 100)
	output := make(map[InputDevice]chan error, 0)
	close := make(map[InputDevice]chan bool, 0)

	var udevCh <-chan *udev.Device
	var cancel context.CancelFunc
	var ctx context.Context

	defer api.Exit()
	u := udev.Udev{}
	if *monitorUdev {
		log.Info("Starting udev monitoring for Bluetooth devices")
		m := u.NewMonitorFromNetlink("udev")
		m.FilterAddMatchSubsystem("bluetooth")

		ctx, cancel = context.WithCancel(context.Background())
		udevCh, _ = m.DeviceChan(ctx)
	}

	go SendKeyboardReports(keyboardInput)
	go SendMouseReports(mouseInput)
	wg.Add(1)
	for {
		select {
		case d := <-udevCh:
			if d.Action() == "add" || d.Action() == "remove" {
				disconnected, err := GetDisconnectedDevices(*adapterId)
				if err != nil {
					log.Errorf("Error checking disconnected devices: %s", err.Error())
				} else {
					for _, device := range disconnected {
						for devId, _ := range output {
							if strings.HasPrefix(devId.Name, device) {
								log.Infof("Disconnected device, stopping listening to: %s (%s)", devId.Name, devId.Device)
								select {
								case close[devId] <- true:
									log.Infof("Sent stop signal to: %s (%s)", devId.Name, devId.Device)
								default:
								}

							}
						}
					}
				}
			}
		default:
		}

		log.Info("Polling for new devices in /dev/input\n")
		devices, _ := evdev.ListInputDevices()
		for _, dev := range devices {
			isMouse := false
			isKeyboard := false
			for k := range dev.Capabilities {
				if k.Name == "EV_REL" {
					isMouse = true
				}
				if k.Name == "EV_KEY" {
					isKeyboard = true
				}
			}
			log.Debugf("Device %s (%s), capabilities: %v (mouse=%t, kbd=%t)", dev.Name, dev.Fn, dev.Capabilities, isMouse, isKeyboard)
			if isKeyboard || isMouse {
				devId := InputDevice{
					Device: dev.Fn,
					Name:   dev.Name,
				}
				if _, ok := output[devId]; !ok {
					output[devId] = make(chan error, 10)
					close[devId] = make(chan bool, 10)
					if isKeyboard && !isMouse && *setupKeyboard {
						go HandleKeyboard(output[devId], keyboardInput, close[devId], uint(*kbdRepeat), uint(*kbdDelay), *dev)
						wg.Add(1)
					}
					log.Debugf("isKeyboard: %t, isMouse: %t, setupMouse: %t", !isKeyboard, isMouse, *setupMouse)
					if isMouse && *setupMouse {
						go HandleMouse(output[devId], mouseInput, close[devId], *dev)
						wg.Add(1)
					}
				}
			}
		}
		time.Sleep(1000 * time.Millisecond)
		for id, eventOutput := range output {
			select {
			case msg := <-eventOutput:
				if msg == nil {
					log.Warnf("Event handler quit: %s", id.Device)
				} else {
					log.Errorf("Received error from %s: %s", id.Device, msg.Error())
				}
				delete(output, id)
				wg.Done()
			default:
			}
		}
	}
	cancel()
}
