package main

import (
	"flag"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/golang/glog"
)

var router = flag.String("router", "bird", "router implementation to use, one of 'bird' or 'quagga'")

func main() {
	flag.Parse()
	if err := installNatRule(); err != nil {
		glog.Exitf("Failed to install NAT rule: %s", err)
	}
	if err := runTCPDump(); err != nil {
		glog.Exitf("Failed to start tcpdump: %s", err)
	}

	if err := writeBirdConfig(); err != nil {
		glog.Exitf("Failed to write bird config: %s", err)
	}
	if err := writeQuaggaConfig(); err != nil {
		glog.Exitf("Failed to write quagga config: %s", err)
	}
	if err := writeGoBGPConfig(); err != nil {
		glog.Exitf("Failed to write gobgp config: %s", err)
	}

	if err := runBird(); err != nil {
		glog.Exitf("Trying to start bird: %s", err)
	}
	if err := runQuagga(); err != nil {
		glog.Exitf("Trying to start quagga: %s", err)
	}
	if err := runGoBGP(); err != nil {
		glog.Exitf("Trying to start gobgp: %s", err)
	}

	http.HandleFunc("/", status)
	http.HandleFunc("/pcap", writePcap)
	http.ListenAndServe(":8080", nil)
}

func nodeIP() string {
	return os.Getenv("METALLB_NODE_IP")
}

func runTCPDump() error {
	if err := os.Mkdir("/run/tcpdump", 0600); err != nil {
		return err
	}
	c := exec.Command("/usr/sbin/tcpdump", "-i", "eth0", "-w", "/run/tcpdump/pcap", "tcp", "port", "1179")
	if err := c.Start(); err != nil {
		return err
	}
	go func() {
		if err := c.Wait(); err != nil {
			glog.Exitf("tcpdump exited with an error: %s", err)
		}
		glog.Exitf("tcpdump exited")
	}()
	return nil
}

func installNatRule() error {
	for _, port := range []int{179, 1179, 2179} {
		c := exec.Command("/sbin/iptables", "-t", "nat", "-A", "INPUT", "-p", "tcp", "--dport", strconv.Itoa(port), "-j", "SNAT", "--to", os.Getenv("METALLB_NODE_IP"))
		if err := c.Run(); err != nil {
			return err
		}
	}
	return nil
}

func runOrCrash(cmd ...string) error {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Start(); err != nil {
		return err
	}
	go func() {
		if err := c.Wait(); err != nil {
			glog.Exitf("%s exited with an error: %s", cmd[0], err)
		}
		glog.Exitf("%s exited", cmd[0])
	}()
	return nil
}
