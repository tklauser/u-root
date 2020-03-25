// Copyright 2017-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/u-root/u-root/pkg/booter"
	"github.com/u-root/u-root/pkg/ipmi"
	"github.com/u-root/u-root/pkg/smbios"
)

var (
	allowInteractive = flag.Bool("i", true, "Allow user to interrupt boot process and run commands")
	doQuiet          = flag.Bool("q", false, "Disable verbose output")
	interval         = flag.Int("I", 1, "Interval in seconds before looping to the next boot command")
	noDefaultBoot    = flag.Bool("nodefault", false, "Do not attempt default boot entries if regular ones fail")
)

var defaultBootsequence = [][]string{
	{"fbnetboot", "-userclass", "linuxboot"},
	{"localboot", "-grub"},
}

// Product list for running IPMI OEM commands
var productList = [2]string{"Tioga Pass", "Mono Lake"}

func isMatched(productName string) bool {
	for _, v := range productList {
		if strings.HasPrefix(productName, v) {
			return true
		}
	}
	return false
}

func getSystemProductName(si *smbios.Info) (string, error) {
	t1, err := si.GetSystemInfo()
	if err != nil {
		log.Printf("Error getting System Information: %v", err)
		return "", err
	}
	return t1.ProductName, nil
}

func getSystemFWVersion(si *smbios.Info) (string, error) {
	t0, err := si.GetBIOSInfo()
	if err != nil {
		log.Printf("Error getting BIOS Information: %v", err)
		return "", err
	}
	return t0.Version, nil
}

func checkCMOSClear(ipmi *ipmi.IPMI) error {
	if cmosclear, bootorder, err := ipmi.IsCMOSClearSet(); cmosclear == true {
		log.Printf("CMOS clear starts")
		if err = cmosClear(); err != nil {
			return err
		}
		// ToDo: Reset RW_VPD to default values
		if err = ipmi.ClearCMOSClearValidBits(bootorder); err != nil {
			return err
		}
		if err = reboot(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

func runIPMICommands() {
	ipmi, err := ipmi.Open(0)
	if err != nil {
		log.Printf("Failed to open ipmi device %v, watchdog may still be running", err)
		return
	}
	defer ipmi.Close()

	if err = ipmi.ShutoffWatchdog(); err != nil {
		log.Printf("Failed to stop watchdog %v.", err)
	} else {
		log.Printf("Watchdog is stopped.")
	}

	// Below IPMI commands would require SMBIOS data
	si, err := smbios.FromSysfs()
	if err != nil {
		log.Printf("Error reading SMBIOS info: %v", err)
		return
	}

	if fwVersion, err := getSystemFWVersion(si); err == nil {
		log.Printf("System firmware version: %s", fwVersion)
		if err = ipmi.SetSystemFWVersion(fwVersion); err != nil {
			log.Printf("Failed to set system firmware version to BMC %v.", err)
		}
	}

	if productName, err := getSystemProductName(si); err == nil {
		if isMatched(productName) {
			log.Printf("Running OEM IPMI commands.")
			if err = checkCMOSClear(ipmi); err != nil {
				log.Printf("IPMI CMOS clear err: %v", err)
			}
		} else {
			log.Printf("No product name is matched for OEM commands.")
		}
	}

}

func main() {
	flag.Parse()

	log.Print(`
                     ____            _                 _                 _   
                    / ___| _   _ ___| |_ ___ _ __ ___ | |__   ___   ___ | |_ 
                    \___ \| | | / __| __/ _ \ '_ ` + "`" + ` _ \| '_ \ / _ \ / _ \| __|
                     ___) | |_| \__ \ ||  __/ | | | | | |_) | (_) | (_) | |_ 
                    |____/ \__, |___/\__\___|_| |_| |_|_.__/ \___/ \___/ \__|
                           |___/
`)
	runIPMICommands()
	sleepInterval := time.Duration(*interval) * time.Second
	if *allowInteractive {
		log.Printf("**************************************************************************")
		log.Print("Starting boot sequence, press CTRL-C within 5 seconds to drop into a shell")
		log.Printf("**************************************************************************")
		time.Sleep(5 * time.Second)
	} else {
		signal.Ignore()
	}

	// Get and show boot entries
	bootEntries := booter.GetBootEntries()
	log.Printf("BOOT ENTRIES:")
	for _, entry := range bootEntries {
		log.Printf("    %v) %+v", entry.Name, string(entry.Config))
	}
	for _, entry := range bootEntries {
		log.Printf("Trying boot entry %s: %s", entry.Name, string(entry.Config))
		if err := entry.Booter.Boot(); err != nil {
			log.Printf("Warning: failed to boot with configuration: %+v", entry)
		}
		if !*doQuiet {
			log.Printf("Sleeping %v before attempting next boot command", sleepInterval)
		}
		time.Sleep(sleepInterval)
	}

	// if boot entries failed, use the default boot sequence
	log.Printf("Boot entries failed")

	if !*noDefaultBoot {
		log.Print("Falling back to the default boot sequence")
		for {
			for _, bootcmd := range defaultBootsequence {
				if !*doQuiet {
					bootcmd = append(bootcmd, "-d")
				}
				log.Printf("Running boot command: %v", bootcmd)
				cmd := exec.Command(bootcmd[0], bootcmd[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					log.Printf("Error executing %v: %v", cmd, err)
				}
			}
			if !*doQuiet {
				log.Printf("Sleeping %v before attempting next boot command", sleepInterval)
			}
			time.Sleep(sleepInterval)
		}
	}
}
