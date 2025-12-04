package main

// docker           run  /bin/bash
// go run main.go   run /bin/bash

import (
	"flag"
	"fmt"
	"os"

	"github.com/Jaffar-Kazmi/JContainer/container"
)

var (
	flagPids    = flag.Int("pids", 10, "maximum number of processess in a container")
	flagMemory  = flag.String("memory", "100M", "memory limit (e.g 50M, 1G)")
	flagCPU     = flag.String("cpu", "50%", "CPU limit as percentage of one core (e.g 50%)")
	flagRootfs  = flag.String("rootfs", "/home/jaffar/jroot", "base rootfs (lowerdir) path")
	flagNetwork = flag.Bool("network", false, "enable network messages with veth pair")
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Println("Usage: main [--pids N --memory 50M --cpu 50% --rootfs PATH] run <command> [args...]")
		os.Exit(1)
	}

	cmd := flag.Arg(0)

	cfg := container.Config{
        Pids:    *flagPids,
        Memory:  *flagMemory,
        CPU:     *flagCPU,
        Rootfs:  *flagRootfs,
        Network: *flagNetwork,
    }

	switch cmd {
	case "run":
		if flag.NArg() < 2 {
			fmt.Println("Usage: main run <command> [args...]")
			os.Exit(1)
		}
		container.Run(cfg,flag.Args()[1:])
	case "child":
		container.Child(cfg, flag.Args()[1:])
	default:
		panic("Bad command. Use 'run' or 'child'")
	}
}
