package container

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

func ensureBridge(name, cidr string) error {
	// 1) Check if bridge already exists
	cmd := exec.Command("ip", "link", "show", name)
	if err := cmd.Run(); err == nil {
		// Exists: make sure it's up and has IP
		exec.Command("ip", "link", "set", name, "up").Run()
		exec.Command("ip", "addr", "add", cidr, "dev", name).Run() // ignore "File exists"
		return nil
	}

	// 2) Create bridge
	if err := exec.Command("ip", "link", "add", "name", name, "type", "bridge").Run(); err != nil {
		return fmt.Errorf("create bridge %s: %w", name, err)
	}
	if err := exec.Command("ip", "addr", "add", cidr, "dev", name).Run(); err != nil {
		return fmt.Errorf("assign ip to bridge %s: %w", name, err)
	}
	if err := exec.Command("ip", "link", "set", name, "up").Run(); err != nil {
		return fmt.Errorf("set bridge %s up: %w", name, err)
	}
	return nil
}

func ensureMasquerade(subnet string) error {
	// Check if rule already exists
	cmd := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING",
		"-s", subnet, "-j", "MASQUERADE")
	if err := cmd.Run(); err == nil {
		return nil // rule already there
	}
	// Add rule
	return exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", subnet, "-j", "MASQUERADE").Run()
}

// detectDefaultInterface returns the name of the active default network interface (e.g., "wlan0", "eth0", "enp3s0").
func detectDefaultInterface() (string, error) {
	out, err := exec.Command("sh", "-c", "ip route get 8.8.8.8 | grep -oP 'dev \\K\\S+'").Output()
	if err != nil {
		return "", fmt.Errorf("detect interface: %w", err)
	}
	return string(out[:len(out)-1]), nil // remove newline
}

func ensureForwardingRules(bridge string) error {
	if err := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run(); err != nil {
		return fmt.Errorf("enable ip_forward: %w", err)
	}

	hostIf, err := detectDefaultInterface()
	if err != nil {
		return fmt.Errorf("cannot detect default interface: %w", err)
	}

	fmt.Printf("Using host interface: %s\n", hostIf)

	check1 := exec.Command("iptables", "-C", "FORWARD", "-i", bridge, "-o", hostIf, "-j", "ACCEPT").Run()
	if check1 != nil {
		exec.Command("iptables", "-I", "FORWARD", "1", "-i", bridge, "-o", hostIf, "-j", "ACCEPT").Run()
	}

	check2 := exec.Command("iptables", "-C", "FORWARD", "-i", hostIf, "-o", bridge,
		"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
	if check2 != nil {
		exec.Command("iptables", "-I", "FORWARD", "1", "-i", hostIf, "-o", bridge,
			"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
	}

	return nil
}

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
	const bridgeName = "jcbr0"
	const bridgeCIDR = "10.0.0.1/24"

	if err := ensureBridge(bridgeName, bridgeCIDR); err != nil {
		return err
	}

	if err := ensureMasquerade("10.0.0.0/24"); err != nil {
		fmt.Printf("warning: failed to set MASQUERADE: %v", err)
	}

	if err := ensureForwardingRules("jcbr0"); err != nil {
		fmt.Printf("warning: failed to set forwarding rules: %v\n", err)
	}

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
