package container

import (
	"fmt"
	"os/exec"
)

func configureContainerVeth(childPID string) error {
	contIf := fmt.Sprintf("veth-cont-%s", childPID)

	fmt.Printf("Configuring container eth0 from %s\n", contIf)

	if err := exec.Command("ip", "link", "set", contIf, "name", "eth0").Run(); err != nil {
		return fmt.Errorf("rename %s -> eth0: %w", contIf, err)
	}

	if err := exec.Command("ip", "addr", "add", "172.18.0.2/24", "dev", "eth0").Run(); err != nil {
		return fmt.Errorf("IP eth0: %w", err)
	}
	if err := exec.Command("ip", "link", "set", "eth0", "up").Run(); err != nil {
		return fmt.Errorf("eth0 up: %w", err)
	}

	// 4. Add default route via host
	if err := exec.Command("ip", "route", "add", "default", "via", "172.18.0.1").Run(); err != nil {
		return fmt.Errorf("default route: %w", err)
	}

	fmt.Printf("Container eth0 (172.18.0.2/24) configured\n")
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