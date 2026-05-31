package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	expect "github.com/google/goexpect"
)

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

var (
	ramFlag      = flag.String("m", "4G", "memory for qemu virtual machine")
	cpuFlag      = flag.String("cpu", "4", "number of cores for the virtual machine")
	debugFlag    = flag.Bool("debug", false, "enable debug output")
	osFlag       = flag.String("os", "9front", "guest operating system")
	archFlag     = flag.String("arch", "amd64", "architecture of vm")
	fsFlag       = flag.String("fs", "hjfs", "filesystem backing the vm disk")
	diskFlag     = flag.String("disk", "", "qcow2 vm disk")
	isoFlag      = flag.String("iso", "", "installer ISO for guests that need one")
	ubootFlag    = flag.String("uboot", "u-boot.bin", "uboot binary for arm64")
	qpathFlag    = flag.String("qpath", "", "optional directory containing qemu binaries")
	drawtermFlag = flag.String("dt", "drawterm", "drawterm binary")
	noguiFlag    = flag.Bool("nogui", false, "disable the GUI")
	freebsdBoot  = flag.String("freebsd-boot", "install", "FreeBSD boot mode: install or disk")
	usbFlags     stringList
)

func init() {
	flag.Var(&usbFlags, "usb", "pass a host USB device through to qemu; repeatable; use bus=BUS,addr=ADDR or vendor=VID,product=PID")
}

func qemuUSBDevice(spec string) (string, error) {
	parts := strings.Split(spec, ",")
	values := make(map[string]string, len(parts))
	for _, part := range parts {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return "", fmt.Errorf("USB spec %q must use key=value fields", spec)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			return "", fmt.Errorf("USB spec %q has an empty key or value", spec)
		}
		values[k] = v
	}

	switch {
	case values["bus"] != "" && values["addr"] != "":
		if len(values) != 2 {
			return "", fmt.Errorf("USB bus/addr spec %q only accepts bus and addr", spec)
		}
		return "usb-host,bus=xhci.0,hostbus=" + values["bus"] + ",hostaddr=" + values["addr"], nil
	case values["vendor"] != "" && values["product"] != "":
		if len(values) != 2 {
			return "", fmt.Errorf("USB vendor/product spec %q only accepts vendor and product", spec)
		}
		return "usb-host,bus=xhci.0,vendorid=" + qemuHexID(values["vendor"]) + ",productid=" + qemuHexID(values["product"]), nil
	default:
		return "", fmt.Errorf("USB spec %q must include bus+addr or vendor+product", spec)
	}
}

func qemuHexID(value string) string {
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		return value
	}
	return "0x" + value
}

func appendUSBDevices(args []string) []string {
	if len(usbFlags) == 0 {
		return args
	}
	args = append(args, "-device", "qemu-xhci,id=xhci")
	for _, usb := range usbFlags {
		device, err := qemuUSBDevice(usb)
		if err != nil {
			log.Fatal(err)
		}
		args = append(args, "-device", device)
	}
	return args
}

func qemu9frontCmd(qcow string) []string {
	m := map[string][]string{
		"amd64": {
			filepath.Join(*qpathFlag, "qemu-system-x86_64"),
			"-nic",
			"user,hostfwd=tcp::17019-:17019",
			"-enable-kvm",
			"-m",
			*ramFlag,
			"-smp",
			*cpuFlag,
			"-nographic",
			"-drive",
			"media=disk,if=virtio,index=0",
		},
		"arm64": {
			filepath.Join(*qpathFlag, "qemu-system-aarch64"),
			"-M",
			"virt-2.12,gic-version=3",
			"-cpu",
			"cortex-a72",
			"-m",
			*ramFlag,
			"-smp",
			*cpuFlag,
			"-bios",
			*ubootFlag,
			"-device",
			"virtio-blk-pci-non-transitional,drive=disk",
			"-nic",
			"user,hostfwd=tcp::17019-:17019,model=virtio-net-pci-non-transitional",
			"-nographic",
			"-drive",
			"if=none,id=disk",
		},
		"386": {
			filepath.Join(*qpathFlag, "qemu-system-x86_64"),
			"-nic",
			"user,hostfwd=tcp::17019-:17019",
			"-enable-kvm",
			"-m",
			*ramFlag,
			"-smp",
			*cpuFlag,
			"-nographic",
			"-drive",
			"media=disk,if=virtio,index=0",
		},
	}
	r, ok := m[*archFlag]
	if !ok {
		log.Fatal("unsupported arch")
	}
	r[len(r)-1] = r[len(r)-1] + ",file=" + qcow
	return appendUSBDevices(r)
}

func qemuFreeBSDCmd(qcow, iso string) []string {
	if *archFlag != "amd64" {
		log.Fatal("FreeBSD currently supports amd64 only")
	}

	args := []string{
		filepath.Join(*qpathFlag, "qemu-system-x86_64"),
		"-enable-kvm",
		"-m",
		*ramFlag,
		"-smp",
		*cpuFlag,
		"-nic",
		"user,hostfwd=tcp::10022-:22",
		"-drive",
		"file=" + qcow + ",if=virtio,media=disk",
	}

	switch *freebsdBoot {
	case "install":
		if iso == "" {
			log.Fatal("FreeBSD install boot requires -iso")
		}
		args = append(args, "-cdrom", iso, "-boot", "order=d")
	case "disk":
		args = append(args, "-boot", "order=c")
	default:
		log.Fatalf("unsupported FreeBSD boot mode: %s", *freebsdBoot)
	}

	if *noguiFlag {
		args = append(args, "-nographic")
	}

	return appendUSBDevices(args)
}

func fatalExpect(errCh <-chan error, context string, err error) {
	select {
	case procErr := <-errCh:
		if procErr != nil {
			log.Fatalf("%s: qemu exited: %v", context, procErr)
		}
	default:
	}
	log.Fatalf("%s: %v", context, err)
}

func mustExpect(exp *expect.GExpect, errCh <-chan error, re string) {
	if _, _, err := exp.Expect(regexp.MustCompile(re), -1); err != nil {
		fatalExpect(errCh, "waiting for "+re, err)
	}
}

func mustSend(exp *expect.GExpect, errCh <-chan error, in string) {
	if err := exp.Send(in); err != nil {
		fatalExpect(errCh, "sending input", err)
	}
}

func diskPath(defaultName string) string {
	var qcow string
	if *diskFlag == "" {
		qcow = defaultName
	} else {
		qcow = *diskFlag
	}
	if _, err := os.Stat(qcow); err != nil {
		fmt.Fprintf(os.Stderr, "could not find %s\n", qcow)
		os.Exit(1)
	}
	return qcow
}

func run9front() {
	qcow := diskPath(fmt.Sprintf("9front.%s.%s.qcow2", *fsFlag, *archFlag))
	qemuArgs := qemu9frontCmd(qcow)
	cm := strings.Join(qemuArgs, " ")
	if *debugFlag {
		fmt.Println(cm)
	}
	exp, errCh, err := expect.SpawnWithArgs(qemuArgs, -1)
	if err != nil {
		log.Fatal(err)
	}
	defer exp.Close()

	if *debugFlag {
		exp.Options(expect.Tee(os.Stdout))
	}
	mustExpect(exp, errCh, "bootargs is")
	mustSend(exp, errCh, "\n")
	mustExpect(exp, errCh, "user")
	mustSend(exp, errCh, "\n")
	mustExpect(exp, errCh, "%")
	mustSend(exp, errCh, `echo 'key proto=dp9ik dom=9front user=glenda !password=password' >/mnt/factotum/ctl`+"\n")
	mustExpect(exp, errCh, "%")
	mustSend(exp, errCh, "ip/ipconfig -6 ether /net/ether0\n")
	mustExpect(exp, errCh, "%")
	mustSend(exp, errCh, "ip/ipconfig ether /net/ether0\n")
	mustExpect(exp, errCh, "%")
	mustSend(exp, errCh, "ip/ipconfig ether /net/ether0 ra6 recvra 1\n")
	mustExpect(exp, errCh, "%")
	mustSend(exp, errCh, "aux/listen1 -t 'tcp!*!17019' /rc/bin/service/tcp17019 &\n")
	mustExpect(exp, errCh, "listen started")

	exitch := make(chan struct{})
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		exitch <- struct{}{}
	}()
	go func() {
		time.Sleep(2 * time.Second)
		if *noguiFlag {
			cmd := exec.Command(*drawtermFlag, "-G", "-r", ".", "-u", "glenda", "-h", "127.0.0.1", "-a", "127.0.0.1", "-c", "service=cpu rc -lI")
			cmd.Env = append(cmd.Environ(), "PASS=password")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			if err != nil {
				log.Println(err)
			}
		} else {
			cmd := exec.Command(*drawtermFlag, "-u", "glenda", "-h", "127.0.0.1", "-a", "127.0.0.1", "-c", "rc", "-c", "console=() service=terminal rc -l")
			cmd.Env = append(cmd.Environ(), "PASS=password")
			cmd.Run()

		}
		exitch <- struct{}{}
	}()
	<-exitch
	mustSend(exp, errCh, "fshalt\n")
	mustExpect(exp, errCh, "done halting")
	exp.Close()
}

func runFreeBSD() {
	qcow := diskPath(fmt.Sprintf("freebsd.%s.qcow2", *archFlag))
	if *freebsdBoot == "install" {
		if *isoFlag == "" {
			log.Fatal("FreeBSD install boot requires -iso")
		}
		if _, err := os.Stat(*isoFlag); err != nil {
			fmt.Fprintf(os.Stderr, "could not find %s\n", *isoFlag)
			os.Exit(1)
		}
	}

	qemuArgs := qemuFreeBSDCmd(qcow, *isoFlag)
	if *debugFlag {
		fmt.Println(strings.Join(qemuArgs, " "))
	}
	cmd := exec.Command(qemuArgs[0], qemuArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()

	switch *osFlag {
	case "9front":
		run9front()
	case "freebsd":
		runFreeBSD()
	default:
		log.Fatalf("unsupported os: %s", *osFlag)
	}
}
