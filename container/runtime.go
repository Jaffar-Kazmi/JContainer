package container

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

type Config struct {
	Pids    int
	Memory  string
	CPU     string
	Rootfs  string
	Network bool
}

func Run(cfg Config, cmdArgs []string) {
	fmt.Printf("Parent: Running %v as PID %d\n", cmdArgs, os.Getpid())

	if len(cmdArgs) < 1 {
		fmt.Println("run: missing command")
		os.Exit(1)
	}

	containerID := strconv.Itoa(os.Getpid())

	// Create cgroup BEFORE starting the process
	cgroupPath := setupCgroup(cfg)

	// Build child args with all flags passed and containerID
	childArgs := []string{
		"--pids", strconv.Itoa(cfg.Pids),
		"--memory", cfg.Memory,
		"--cpu", cfg.CPU,
		"--rootfs", cfg.Rootfs,
	}

	if cfg.Network {
		childArgs = append(childArgs, "--network")
	}

	childArgs = append(childArgs, "child", cgroupPath, containerID)
	childArgs = append(childArgs, cmdArgs...)

	cmd := exec.Command("/proc/self/exe", childArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cloneflags := syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS
	if cfg.Network {
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
	if cfg.Network {
		if err := setupContainerVeth(childPID, containerID); err != nil {
			fmt.Printf("Network setup failed: %v\n", err)
		}
	}

	// Wait for container process to exit
	must(cmd.Wait())

	// Cleanup
	if cfg.Network {
		cleanupVeth(containerID)
	}
	cleanupCgroup(cgroupPath)
	cleanupOverlay(containerID)
}

func Child(cfg Config, args []string) {
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

	if cfg.Network {
		if err := configureContainerVeth(containerID); err != nil {
			fmt.Printf("Container Network setup failed: %v\n", err)
		}
	}

	mergedDir, err := settupOverlayFS(containerID, cfg)
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
