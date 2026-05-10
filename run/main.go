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
	archFlag     = flag.String("arch", "amd64", "architecture of vm")
	fsFlag       = flag.String("fs", "hjfs", "filesystem backing the vm disk")
	diskFlag     = flag.String("disk", "", "qcow2 vm disk")
	ubootFlag    = flag.String("uboot", "u-boot.bin", "uboot binary for arm64")
	qpathFlag    = flag.String("qpath", "", "optional directory containing qemu binaries")
	drawtermFlag = flag.String("dt", "drawterm", "drawterm binary")
	noguiFlag    = flag.Bool("nogui", false, "disable the GUI")
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
		return "usb-host,hostbus=" + values["bus"] + ",hostaddr=" + values["addr"], nil
	case values["vendor"] != "" && values["product"] != "":
		if len(values) != 2 {
			return "", fmt.Errorf("USB vendor/product spec %q only accepts vendor and product", spec)
		}
		return "usb-host,vendorid=" + qemuHexID(values["vendor"]) + ",productid=" + qemuHexID(values["product"]), nil
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

func qemuCmd(qcow string) []string {
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
	if len(usbFlags) > 0 {
		r = append(r, "-usb")
		for _, usb := range usbFlags {
			device, err := qemuUSBDevice(usb)
			if err != nil {
				log.Fatal(err)
			}
			r = append(r, "-device", device)
		}
	}
	return r
}

func main() {
	flag.Parse()

	var qcow string
	if *diskFlag == "" {
		qcow = fmt.Sprintf("9front.%s.%s.qcow2", *fsFlag, *archFlag)
	} else {
		qcow = *diskFlag
	}
	if _, err := os.Stat(qcow); err != nil {
		fmt.Fprintf(os.Stderr, "could not find %s\n", qcow)
		os.Exit(1)
	}

	cm := strings.Join(qemuCmd(qcow), " ")
	if *debugFlag {
		fmt.Println(cm)
	}
	exp, _, err := expect.SpawnWithArgs(qemuCmd(qcow), -1)
	if err != nil {
		log.Fatal(err)
	}
	defer exp.Close()

	if *debugFlag {
		exp.Options(expect.Tee(os.Stdout))
	}
	exp.Expect(regexp.MustCompile("bootargs is"), -1)
	exp.Send("\n")
	exp.Expect(regexp.MustCompile("user"), -1)
	exp.Send("\n")
	exp.Expect(regexp.MustCompile("%"), -1)
	exp.Send(`echo 'key proto=dp9ik dom=9front user=glenda !password=password' >/mnt/factotum/ctl` + "\n")
	exp.Expect(regexp.MustCompile("%"), -1)
	exp.Send("ip/ipconfig -6 ether /net/ether0\n")
	exp.Expect(regexp.MustCompile("%"), -1)
	exp.Send("ip/ipconfig ether /net/ether0\n")
	exp.Expect(regexp.MustCompile("%"), -1)
	exp.Send("ip/ipconfig ether /net/ether0 ra6 recvra 1\n")
	exp.Expect(regexp.MustCompile("%"), -1)
	exp.Send("aux/listen1 -t 'tcp!*!17019' /rc/bin/service/tcp17019 &\n")
	exp.Expect(regexp.MustCompile("listen started"), -1)

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
	exp.Send("fshalt\n")
	exp.Expect(regexp.MustCompile("done halting"), -1)
	exp.Close()
}
