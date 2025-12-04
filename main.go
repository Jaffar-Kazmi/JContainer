package main

// docker           run  /bin/bash
// go run main.go   run /bin/bash

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

var (
	flagPids    = flag.Int("pids", 10, "maximum number of processess in a container")
	flagMemory  = flag.String("memory", "100M", "memory limit (e.g 50M, 1G)")
	flagCPU     = flag.String("cpu", "50%", "CPU limit as percentage of one core (e.g 50%)")
	flagRootfs  = flag.String("rootfs", "/home/jaffar/Documents/Lectures/OSLabs/Project/jroot", "base rootfs (lowerdir) path")
	flagNetwork = flag.Bool("network", false, "enable network messages with veth pair")
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Println("Usage: main [--pids N --memory 50M --cpu 50% --rootfs PATH] run <command> [args...]")
		os.Exit(1)
	}

	cmd := flag.Arg(0)

	switch cmd {
	case "run":
		if flag.NArg() < 2 {
			fmt.Println("Usage: main run <command> [args...]")
			os.Exit(1)
		}
		run(flag.Args()[1:])
	case "child":
		child(flag.Args()[1:])
	default:
		panic("Bad command. Use 'run' or 'child'")
	}
}

func run(cmdArgs []string) {
	fmt.Printf("Parent: Running %v as PID %d\n", cmdArgs, os.Getpid())

	if len(cmdArgs) < 1 {
		fmt.Println("run: missing command")
		os.Exit(1)
	}

	containerID := strconv.Itoa(os.Getpid())

	// Create cgroup BEFORE starting the process
	cgroupPath := setupCgroup()

	// Build child args with all flags passed and containerID
	childArgs := []string{
		"--pids", strconv.Itoa(*flagPids),
		"--memory", *flagMemory,
		"--cpu", *flagCPU,
		"--rootfs", *flagRootfs,
	}

	if *flagNetwork {
		childArgs = append(childArgs, "--network")
	}

	childArgs = append(childArgs, "child", cgroupPath, containerID)
	childArgs = append(childArgs, cmdArgs...)

	cmd := exec.Command("/proc/self/exe", childArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cloneflags := syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS
	if *flagNetwork {
		cloneflags |= syscall.CLONE_NEWNET
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: uintptr(cloneflags),
	}

	// Start the process
	must(cmd.Start())
	childPID := cmd.Process.Pid
	fmt.Printf("Parent: Started child process PID=%d (container %s), waiting for completion\n", childPID, containerID)

	// Setup networking if enabled (use child PID for netns)
	if *flagNetwork {
		if err := setupContainerVeth(childPID, containerID); err != nil {
			fmt.Printf("Network setup failed: %v\n", err)
		}
	}

	// Wait for container process to exit
	must(cmd.Wait())

	// Cleanup
	if *flagNetwork {
		cleanupVeth(containerID)
	}
	cleanupCgroup(cgroupPath)
	cleanupOverlay(containerID)
}

func child(args []string) {
	fmt.Printf("Child: Running %v as PID %d\n", args, os.Getpid())

	if len(args) < 3 {
		panic("Usage: child <cgroup> <containerID> <command> [args...]")
	}

	cgroupPath := args[0]
	containerID := args[1]
	command := args[2]
	cmdArgs := args[3:]

	pid := os.Getpid()

	// Write our PID to the cgroup
	must(os.WriteFile(cgroupPath+"/cgroup.procs", []byte(strconv.Itoa(pid)), 0644))
	fmt.Printf("Child: Added self (PID %d) to cgroup %s\n", pid, cgroupPath)

	// Set hostname in new UTS namespace
	must(syscall.Sethostname([]byte("jcontainer")))

	if *flagNetwork {
		if err := configureContainerVeth(containerID); err != nil {
			fmt.Printf("Container Network setup failed: %v\n", err)
		}
	}

	mergedDir, err := settupOverlayFS(containerID)
	must(err)
	fmt.Printf("Child: Using overlay merged dir %s as new root\n", mergedDir)

	// Change root filesystem to merged overlay
	must(syscall.Chroot(mergedDir))
	must(syscall.Chdir("/"))

	// Mount proc filesystem
	must(syscall.Mount("proc", "proc", "proc", 0, ""))

	// Run the actual command
	cmd := exec.Command(command, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	must(cmd.Run())

	// Cleanup
	must(syscall.Unmount("/proc", 0))
}

func setupCgroup() string {
	cgroupBase := "/sys/fs/cgroup"
	cgroupName := fmt.Sprintf("jcontainer-%d", os.Getpid())
	cgroupPath := cgroupBase + "/" + cgroupName

	os.RemoveAll(cgroupPath)

	// Create cgroup directory
	must(os.MkdirAll(cgroupPath, 0755))
	fmt.Println("Created cgroup:", cgroupPath)

	// Set PID limit to 10 processes
	must(os.WriteFile(cgroupPath+"/pids.max", []byte(strconv.Itoa(*flagPids)), 0644))

	// Add memory limit
	memBytes := parseMemoryLimit(*flagMemory)
	must(os.WriteFile(cgroupPath+"/memory.max", []byte(strconv.Itoa(memBytes)), 0644))

	// Add CPU limit
	quota, period := parseCPULimit(*flagCPU)
	cpuMax := fmt.Sprintf("%d %d", quota, period)
	must(os.WriteFile(cgroupPath+"/cpu.max", []byte(cpuMax), 0644))

	return cgroupPath
}

func cleanupCgroup(cgroupPath string) {
	// Remove the cgroup directory
	os.Remove(cgroupPath)
	fmt.Println("Cleaned up cgroup:", cgroupPath)
}

func parseMemoryLimit(s string) int {
	if len(s) == 0 {
		return 104857600
	}
	last := s[len(s)-1]
	num := s[:len(s)-1]

	switch last {
	case 'M', 'm':
		v, _ := strconv.Atoi(num)
		return v * 1024 * 1024
	case 'G', 'g':
		v, _ := strconv.Atoi(num)
		return v * 1024 * 1024 * 1024
	default:
		v, _ := strconv.Atoi(s)
		return v
	}
}

func parseCPULimit(s string) (quota, period int) {
	if len(s) == 0 {
		return 50000, 100000
	}

	if s[len(s)-1] == '%' {
		v, _ := strconv.Atoi(s[:len(s)-1])
		period = 100000
		quota = v * period / 100
		return quota, period
	}

	var q, p int
	fmt.Sscanf(s, "%d:%d", &q, &p)
	if q == 0 || p == 0 {
		return 50000, 100000
	}
	return q, p
}

func settupOverlayFS(containerID string) (string, error) {
	fmt.Println("DEBUG: flagRootfs =", *flagRootfs)
	baseDir := *flagRootfs
	lowerDir := baseDir

	info, err := os.Stat(lowerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("lowerdir %s does not exist", lowerDir)
		}
		return "", fmt.Errorf("error checking lowerdir %s: %v", lowerDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("lowerdir %s is not a directory", lowerDir)
	}

	// overlay directories on host

	overlayBase := fmt.Sprintf("/tmp/jcontainer-%s", containerID)
	upperDir := overlayBase + "/upper"
	workDir := overlayBase + "/work"
	mergedDir := overlayBase + "/merged"

	// Debug prints
	fmt.Println("overlay lower =", lowerDir)
	fmt.Println("overlay upper =", upperDir)
	fmt.Println("overlay work  =", workDir)
	fmt.Println("overlay merged=", mergedDir)

	for _, dir := range []string{upperDir, workDir, mergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}

	// Mount overlay FS
	options := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
	fmt.Println("overlay options:", options)

	if err := syscall.Mount("overlay", mergedDir, "overlay", 0, options); err != nil {
		return "", fmt.Errorf("overlay mount failed: %v", err)
	}

	fmt.Printf("OverlayFS mounted: lower=%s upper=%s work=%s merged=%s\n", lowerDir, upperDir, workDir, mergedDir)

	return mergedDir, nil
}

func cleanupOverlay(containerID string) {
	overlayBase := fmt.Sprintf("/tmp/jcontainer-%s", containerID)
	mergedDir := overlayBase + "/merged"

	_ = syscall.Unmount(mergedDir, syscall.MNT_DETACH)

	_ = os.RemoveAll(overlayBase)

	fmt.Printf("Cleaned up overlay: %s\n", overlayBase)

}

func setupContainerVeth(childPID int, containerID string) error {
	hostIf := fmt.Sprintf("veth-host-%s", containerID)
	contIf := fmt.Sprintf("veth-cont-%s", containerID)

	fmt.Printf("Creating veth pair: %s <-> %s\n", hostIf, contIf)

	// 1. Create veth pair
	if err := exec.Command("ip", "link", "add", hostIf, "type", "veth", "peer", "name", contIf).Run(); err != nil {
		return fmt.Errorf("create veth pair: %w", err)
	}

	// 2. Move container end to child netns (use childPID only here)
	if err := exec.Command("ip", "link", "set", contIf, "netns", strconv.Itoa(childPID)).Run(); err != nil {
		return fmt.Errorf("move %s to netns %d: %w", contIf, childPID, err)
	}

	// 3. Configure host side
	if err := exec.Command("ip", "addr", "add", "172.18.0.1/24", "dev", hostIf).Run(); err != nil {
		return fmt.Errorf("host IP %s: %w", hostIf, err)
	}
	if err := exec.Command("ip", "link", "set", hostIf, "up").Run(); err != nil {
		return fmt.Errorf("host up %s: %w", hostIf, err)
	}

	fmt.Printf("Host veth %s (172.18.0.1/24) → container netns %d\n", hostIf, childPID)
	return nil
}

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
