# go-hidproxy

Proxies Bluetooth keyboards and mouse as HID devices (eg. with Raspberry Zero Pi W)

## Build

Build with Go 1.11+.

## Install

- Copy binary to `/usr/sbin/go-hidproxy`
- Install systemd unit file to `/etc/systemd/system`
- Reload daemons: `sudo systemctl daemon-reload`
- Enable hidproxy: `sudo systemctl enable hidproxy`
- (Optionally) Start hidproxy: `sudo systemctl start hidproxy`

## Raspberry Pi Zero W setup

I used a pretty standard Raspbian image:
```
Distributor ID: Raspbian
Description:    Raspbian GNU/Linux 10 (buster)
Release:        10
Codename:       buster
```

You'll need to setup `/boot/config.txt` with:
````
dtoverlay=dwc2
````

In `/etc/modules` you should have:
```
dwc2
libcomposite
evdev
```


## Pair Bluetooth keyboard

One time pairing:

```
# sudo bluetoothctl
> discoverable on
> pairable on
> agent NoInputNoOutput
> default-agent
> connect aa:bb:cc:dd:ee:ff
> trust aa:bb:cc:dd:ee:ff
```