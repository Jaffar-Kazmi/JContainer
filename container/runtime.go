package container

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func Run(cfg Config, cmdArgs []string) {
	fmt.Printf("Parent: Running %v as PID %d\n", cmdArgs, os.Getpid())

	if len(cmdArgs) < 1 {
		fmt.Println("run: missing command")
		os.Exit(1)
	}

	// Use parent PID as container ID
	parentPID := os.Getpid()
	containerID := strconv.Itoa(parentPID)

	// 1) Create cgroup BEFORE starting the process
	cgroupPath := setupCgroup(cfg)

	// 2) Compute container IP + gateway if networking is enabled
	var ipCIDR, ipBare, gateway string
	if cfg.Network {
		hostOctet := 10 + (parentPID % 200)
		ipBare = fmt.Sprintf("10.0.0.%d", hostOctet)
		ipCIDR = ipBare + "/24"
		gateway = "10.0.0.1"
	}

	// 3) Build child args with flags and container params
	childArgs := []string{
		"--pids", strconv.Itoa(cfg.Pids),
		"--memory", cfg.Memory,
		"--cpu", cfg.CPU,
		"--rootfs", cfg.Rootfs,
	}
	if cfg.Network {
		childArgs = append(childArgs, "--network")
	}

	// child <cgroupPath> <containerID> [<ipCIDR> <gateway>] mmand> [args...]
	childArgs = append(childArgs, "child", cgroupPath, containerID)
	if cfg.Network {
		childArgs = append(childArgs, ipCIDR, gateway)
	}
	childArgs = append(childArgs, cmdArgs...)

	cmd := exec.Command("/proc/self/exe", childArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 4) Set namespaces
	cloneflags := syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS
	if cfg.Network {
		cloneflags |= syscall.CLONE_NEWNET
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: uintptr(cloneflags),
	}

	// 5) Start child
	must(cmd.Start())
	childPID := cmd.Process.Pid
	fmt.Printf("Parent: Started child process PID=%d (container %s), waiting for completion\n",
		childPID, containerID)

	// 6) Host-side networking
	if cfg.Network {
		if err := setupContainerVeth(childPID, containerID); err != nil {
			fmt.Printf("Network setup failed: %v\n", err)
		}

		if cfg.HostIP != "" {
			for _, pm := range cfg.Publishes {
				if err := addPortPublishRule(ipBare, pm, cfg.HostIP); err != nil {
					fmt.Printf("Port publish failed: %v\n", err)
				}
			}
		} else if len(cfg.Publishes) > 0 {
			fmt.Println("HostIP is empty, skipping --publish rules")
		}
	}

	mergedDir := fmt.Sprintf("/tmp/jcontainer-%s/merged", containerID)
    state := ContainerState {
        ID:        containerID,
        InitPID:   childPID,
        IP:        ipBare,
        CreatedAt: time.Now(),
        Command:   cmdArgs,
        MergedDir: mergedDir,
    }

	if err := writeState(&state); err != nil {
        fmt.Printf("failed to write state: %v\n", err)
    }


	// 7) Wait and cleanup
	must(cmd.Wait())

	if cfg.Network {
		if cfg.HostIP != "" {
			for _, pm := range cfg.Publishes {
				deletePortPublishRule(ipBare, pm, cfg.HostIP)
			}
		}
		cleanupVeth(containerID)
	}
	cleanupCgroup(cgroupPath)
	cleanupOverlay(containerID)
}

func Child(cfg Config, args []string) {
	fmt.Printf("Child: Running %v as PID %d\n", args, os.Getpid())

	if len(args) < 3 {
		panic("Usage: child <cgroup> <containerID> [<ipCIDR> <gateway>] <command> [args...]")
	}

	cgroupPath := args[0]
	containerID := args[1]

	var ipCIDR, gateway, command string
	var cmdArgs []string

	if cfg.Network {
		if len(args) < 5 {
			panic("Usage: child <cgroup> <containerID> <ipCIDR> <gateway> <command> [args...]")
		}
		ipCIDR = args[2]
		gateway = args[3]
		command = args[4]
		cmdArgs = args[5:]
	} else {
		command = args[2]
		cmdArgs = args[3:]
	}

	pid := os.Getpid()

	// Write our PID to the cgroup
	must(os.WriteFile(cgroupPath+"/cgroup.procs", []byte(strconv.Itoa(pid)), 0644))
	fmt.Printf("Child: Added self (PID %d) to cgroup %s\n", pid, cgroupPath)

	// Set hostname in new UTS namespace
	must(syscall.Sethostname([]byte("jcontainer")))

	if cfg.Network {
		if err := configureContainerVeth(containerID, ipCIDR, gateway); err != nil {
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
