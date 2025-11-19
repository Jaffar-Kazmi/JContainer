package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// docker           run image <command> <params>
// go run main.go	run		  <command> <params>
// pwd: /home/jaffar/Documents/Lectures/OSLabs/Project
// root directory for container: /home/jaffar/Documents/Lectures/OSLabs/Project/jroot/

func main() {
	switch os.Args[1] {
	case "run":
		run()
	case "child":
		child()

	default:
		panic("bad command")
	}
}

func run() {
	fmt.Printf("Running %v as %d\n", os.Args[2:], os.Getpid())

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	cmd.Run()
}

func child() {
	fmt.Printf("Running %v as %d\n", os.Args[2:], os.Getpid())

	must(syscall.Sethostname([]byte("jcontainer")))
	must(syscall.Chroot("/home/jaffar/Documents/Lectures/OSLabs/Project/jroot/"))
	must(syscall.Chdir("/"))
	must(syscall.Mount("proc", "proc", "proc", 0, ""))

	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	must(cmd.Run())
	must(syscall.Unmount("/proc", 0))

}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
