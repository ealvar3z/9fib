## 9fib (de-nixified fork of [majiru's 9front-in-a-box](https://github.com/majiru/9front-in-a-box))

This repo automates setup and local execution of a 9front virtual machine on
Arch Linux. Virtual machines run under qemu and connect through drawterm.

The current non-Nix workflow is intentionally narrow:

- host OS: Arch / Omarchy
- guest arch: amd64
- filesystems: hjfs, cwfs, gefs

It is meant to make trying 9front easy, not to be a general-purpose 9front
server deployment tool.

## Dependencies

Install the host packages first:

```bash
sudo pacman -S --needed go qemu-full expect plan9port
```

`plan9port` provides the `drawterm` binary used to connect to the guest.

## Setup

Prepare a local disk image with:

```bash
bin/setup-vm
```

That creates `9front.hjfs.amd64.qcow2` in the current directory.

The base release image is cached under:

```text
${XDG_CACHE_HOME:-$HOME/.cache}/9fib/releases
```

The default `hjfs` disk is a qcow2 overlay backed by that cached release image.
If you delete the cached release file, recreate the VM with `bin/setup-vm`.

Useful variants:

```bash
bin/setup-vm --fs cwfs
bin/setup-vm --fs gefs
bin/setup-vm --latest
bin/setup-vm --release 11554
bin/setup-vm --disk ./my-9front.qcow2
```

## Running

Start the VM and connect with drawterm:

```bash
bin/run-vm
```

The default account is `glenda` with password `password`.

For a terminal-only session:

```bash
bin/run-vm --nogui
```

To select another filesystem image or drawterm binary:

```bash
bin/run-vm --fs cwfs
bin/run-vm --drawterm /usr/bin/drawterm
```

To pass a host USB device through to 9front, identify it with `lsusb`, then
use either its bus/address or vendor/product IDs:

```bash
bin/run-vm --usb bus=1,addr=4
bin/run-vm --usb vendor=046d,product=c52b
```

`--usb` can be repeated for multiple devices. Your user must have permission
to access the host USB device, usually through udev rules or by running QEMU
with sufficient privileges.

Extra flags are passed through to the Go launcher in `run/`, so existing flags
such as `-debug`, `-m`, or `-cpu` can still be used:

```bash
bin/run-vm --nogui -debug -m 2G
```

Instructions for using rio are in the [9front FQA](http://fqa.9front.org/fqa8.html).

## Notes

- `arm64` and `386` are not part of the current non-Nix workflow.
- `--latest` uses the rolling `https://build.9front.org/9front` build stream.
- The default release pin remains `11554` until it is updated explicitly.
