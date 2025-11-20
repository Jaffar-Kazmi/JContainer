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

	containerID := strconv.Itoa(os.Getpid())

	// Create cgroup BEFORE starting the process
	cgroupPath := setupCgroup()

	cmd := exec.Command(
		"/proc/self/exe", 
		append([]string{"child", cgroupPath, containerID}, os.Args[2:]...)...)
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
	cleanupOverlay(containerID)
}

func child() {
	fmt.Printf("Child: Running %v as PID %d\n", os.Args[2:], os.Getpid())

	if len(os.Args) < 5 {
		panic("Usage: child <cgroup> <containerID> <command> [args...]")
	}

	cgroupPath := os.Args[2]
	containerID := os.Args[3]
	command := os.Args[4]
	args := os.Args[5:]

	pid := os.Getpid()

	// Write our PID to the cgroup
	must(os.WriteFile(cgroupPath+"/cgroup.procs", []byte(strconv.Itoa(pid)), 0644))
	fmt.Printf("Child: Added self (PID %d) to cgroup %s\n", pid, cgroupPath)

	// Set hostname in new UTS namespace
	must(syscall.Sethostname([]byte("jcontainer")))

	mergedDir, err := settupOverlayFS(containerID)
	must(err)
	fmt.Printf("Child: Using overlay merged dir %s as new root\n", mergedDir)


	// Change root filesystem to merged overlay
	must(syscall.Chroot(mergedDir))
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

	// Add memory limit
	must(os.WriteFile(cgroupPath + "/memory.max", []byte("104857600"), 0644))

	// Add CPU limit
	must(os.WriteFile(cgroupPath + "/cpu.max", []byte("50000 100000"), 0644))

	return cgroupPath
}

func cleanupCgroup(cgroupPath string) {
	// Remove the cgroup directory
	os.Remove(cgroupPath)
	fmt.Println("Cleaned up cgroup:", cgroupPath)
}

func settupOverlayFS(containerID string) (string, error) {
	// Base path
	baseDir := "/home/jaffar/Documents/Lectures/OSLabs/Project"
	lowerDir := baseDir + "/jroot"  // lower read only image

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

func must(err error) {
	if err != nil {
		panic(err)
	}
}
