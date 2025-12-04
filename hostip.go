// hostip.go
package main

import (
    "log"
    "net"
)

func getHostIP() string {
    iface, err := net.InterfaceByName("wlan0")
    if err != nil {
        log.Printf("warning: interface wlan0 not found: %v; disabling publish", err)
        return ""
    }

    addrs, err := iface.Addrs()
    if err != nil {
        log.Printf("warning: cannot get addrs for wlan0: %v; disabling publish", err)
        return ""
    }

    for _, addr := range addrs {
        ipNet, ok := addr.(*net.IPNet)
        if !ok {
            continue
        }
        ip := ipNet.IP.To4()
        if ip == nil {
            continue
        }
        if ip.IsLoopback() {
            continue
        }
        // This should be your 192.168.100.x
        return ip.String()
    }

    log.Println("warning: wlan0 has no IPv4 address; disabling publish")
    return ""
}
