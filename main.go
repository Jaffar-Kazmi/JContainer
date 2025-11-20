package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go run <command> [args...]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		run()
	case "child":
		child()
	default:
		panic("Bad command. Use 'run' or 'child'")
	}
}

func run() {
	fmt.Printf("Parent: Running %v as PID %d\n", os.Args[2:], os.Getpid())

	// Create cgroup BEFORE starting the process
	cgroupPath := setupCgroup()

	cmd := exec.Command("/proc/self/exe", append([]string{"child", cgroupPath}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr


	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS,
	}

	// Start the process
	must(cmd.Start())

	fmt.Printf("Parent: Started child process, waiting for completion\n")

	// Now wait for it to finish
	must(cmd.Wait())

	// Cleanup cgroup
	cleanupCgroup(cgroupPath)
}

func child() {
	fmt.Printf("Child: Running %v as PID %d\n", os.Args[2:], os.Getpid())

	if len(os.Args) < 4 {
		panic("Usage: child <cgroup> <command> [args...]")
	}

	cgroupPath := os.Args[2]
	command := os.Args[3]
	args := os.Args[4:]

	pid := os.Getpid()

	// Write our PID to the cgroup
	must(os.WriteFile(cgroupPath+"/cgroup.procs", []byte(strconv.Itoa(pid)), 0644))
	fmt.Printf("Child: Added self (PID %d) to cgroup %s\n", pid, cgroupPath)

	// Set hostname in new UTS namespace
	must(syscall.Sethostname([]byte("jcontainer")))

	// Change root filesystem
	must(syscall.Chroot("/home/jaffar/Documents/Lectures/OSLabs/Project/jroot/"))
	must(syscall.Chdir("/"))

	// Mount proc filesystem
	must(syscall.Mount("proc", "proc", "proc", 0, ""))

	// Run the actual command
	cmd := exec.Command(command, args...)
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
	must(os.WriteFile(cgroupPath+"/pids.max", []byte("10"), 0644))

	return cgroupPath
}

func cleanupCgroup(cgroupPath string) {
	// Remove the cgroup directory
	os.Remove(cgroupPath)
	fmt.Println("Cleaned up cgroup:", cgroupPath)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
