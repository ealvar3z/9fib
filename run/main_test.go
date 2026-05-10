package main

import "testing"

func TestQEMUUSBDeviceBusAddress(t *testing.T) {
	got, err := qemuUSBDevice("bus=1,addr=4")
	if err != nil {
		t.Fatal(err)
	}
	want := "usb-host,hostbus=1,hostaddr=4"
	if got != want {
		t.Fatalf("qemuUSBDevice() = %q, want %q", got, want)
	}
}

func TestQEMUUSBDeviceVendorProduct(t *testing.T) {
	got, err := qemuUSBDevice("vendor=046d,product=c52b")
	if err != nil {
		t.Fatal(err)
	}
	want := "usb-host,vendorid=0x046d,productid=0xc52b"
	if got != want {
		t.Fatalf("qemuUSBDevice() = %q, want %q", got, want)
	}
}

func TestQEMUUSBDeviceRejectsIncompleteSpec(t *testing.T) {
	if _, err := qemuUSBDevice("bus=1"); err == nil {
		t.Fatal("qemuUSBDevice() succeeded with incomplete spec")
	}
}
