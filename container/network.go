package container

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

func addPortPublishRule(containerIP string, pm PortMapping, hostIP string) error {
    if hostIP == "" {
        return fmt.Errorf("no host IP available")
    }
    hostPort := strconv.Itoa(pm.HostPort)
    dest := fmt.Sprintf("%s:%d", containerIP, pm.ContainerPort)

    // Local + remote traffic to hostIP:hostPort
    if err := exec.Command("iptables", "-t", "nat", "-I", "OUTPUT", "1",
        "-p", pm.Protocol,
        "-d", hostIP, "--dport", hostPort,
        "-j", "DNAT", "--to-destination", dest).Run(); err != nil {
        return fmt.Errorf("OUTPUT DNAT: %w", err)
    }

    if err := exec.Command("iptables", "-t", "nat", "-I", "PREROUTING", "1",
        "-p", pm.Protocol,
        "-d", hostIP, "--dport", hostPort,
        "-j", "DNAT", "--to-destination", dest).Run(); err != nil {
        return fmt.Errorf("PREROUTING DNAT: %w", err)
    }

    fmt.Printf("Published %s:%d -> %s\n", hostIP, pm.HostPort, dest)
    return nil
}

func deletePortPublishRule(containerIP string, pm PortMapping, hostIP string) {
    if hostIP == "" {
        return
    }
    hostPort := strconv.Itoa(pm.HostPort)
    dest := fmt.Sprintf("%s:%d", containerIP, pm.ContainerPort)

    _ = exec.Command("iptables", "-t", "nat", "-D", "PREROUTING",
        "-p", pm.Protocol,
        "-d", hostIP, "--dport", hostPort,
        "-j", "DNAT", "--to-destination", dest).Run()

    _ = exec.Command("iptables", "-t", "nat", "-D", "OUTPUT",
        "-p", pm.Protocol,
        "-d", hostIP, "--dport", hostPort,
        "-j", "DNAT", "--to-destination", dest).Run()
}

func setupContainerVeth(childPID int, containerID string) error {
	hostIf := fmt.Sprintf("veth-host-%s", containerID)
	contIf := fmt.Sprintf("veth-cont-%s", containerID)

	fmt.Printf("Creating veth pair: %s <-> %s\n", hostIf, contIf)

	// 1. Create veth pair
	if err := exec.Command("ip", "link", "add", hostIf, "type", "veth", "peer", "name", contIf).Run(); err != nil {
		return fmt.Errorf("create veth pair: %w", err)
	}

	// 2. Attach host end to bridge jcbr0
	if err := exec.Command("ip", "link", "set", hostIf, "master", "jcbr0").Run(); err != nil {
		return fmt.Errorf("attach %s to jcbr0: %w", hostIf, err)
	}

	// 3. Move container end to child netns (use childPID only here)
	if err := exec.Command("ip", "link", "set", contIf, "netns", strconv.Itoa(childPID)).Run(); err != nil {
		return fmt.Errorf("move %s to netns %d: %w", contIf, childPID, err)
	}

	// 4. Configure host side
	if err := exec.Command("ip", "link", "set", hostIf, "up").Run(); err != nil {
		return fmt.Errorf("bring %s up: %w", hostIf, err)
	}

	fmt.Printf("Host veth %s attached to jcbr0 → netns %d\n", hostIf, childPID)
	return nil
}

func configureContainerVeth(containerID, ipCIDR, gateway string) error {
    contIf := fmt.Sprintf("veth-cont-%s", containerID)
    fmt.Printf("Configuring container eth0 from %s\n", contIf)

    // Wait for veth to appear in this netns
    const maxTries = 20
    for i := 0; i < maxTries; i++ {
        if err := exec.Command("ip", "link", "show", contIf).Run(); err == nil {
            break
        }
        time.Sleep(100 * time.Millisecond)
        if i == maxTries-1 {
            return fmt.Errorf("interface %s not found after waiting", contIf)
        }
    }

    // 1. Rename veth-cont -> eth0
    if err := exec.Command("ip", "link", "set", contIf, "name", "eth0").Run(); err != nil {
        return fmt.Errorf("rename %s -> eth0: %w", contIf, err)
    }

    // 2. Assign IP
    if err := exec.Command("ip", "addr", "add", ipCIDR, "dev", "eth0").Run(); err != nil {
        return fmt.Errorf("IP eth0: %w", err)
    }

    // 3. Bring up
    if err := exec.Command("ip", "link", "set", "eth0", "up").Run(); err != nil {
        return fmt.Errorf("eth0 up: %w", err)
    }

    // 4. Default route
    if err := exec.Command("ip", "route", "add", "default", "via", gateway).Run(); err != nil {
        return fmt.Errorf("default route: %w", err)
    }

    fmt.Printf("Container eth0 (%s) configured via %s\n", ipCIDR, gateway)
    return nil
}

func cleanupVeth(containerID string) {
	hostIf := fmt.Sprintf("veth-host-%s", containerID)
	_ = exec.Command("ip", "link", "del", hostIf).Run()
	fmt.Printf("Cleaned up veth: %s\n", hostIf)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}