package main

// sudo docker           run /bin/bash
// sudo go run main.go   run /bin/bash

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Jaffar-Kazmi/JContainer/container"
)

type publishFlags []string

func (p *publishFlags) String() string {
	return strings.Join(*p, ",")
}

func (p *publishFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

var (
	flagPids      = flag.Int("pids", 10, "maximum number of processess in a container")
	flagMemory    = flag.String("memory", "100M", "memory limit (e.g 50M, 1G)")
	flagCPU       = flag.String("cpu", "50%", "CPU limit as percentage of one core (e.g 50%)")
	flagRootfs    = flag.String("rootfs", "/home/jaffar/jroot", "base rootfs (lowerdir) path")
	flagNetwork   = flag.Bool("network", false, "enable network messages with veth pair")
	flagPublishes publishFlags
)

func main() {
	flag.Var(&flagPublishes, "publish", "publish a port (format: hostPort:containerPort, e.g. 8080:80)")
	flag.Parse()
	hostIP := getHostIP()

	if flag.NArg() < 1 {
		fmt.Println("Usage: main [--pids N --memory 50M --cpu 50% --rootfs PATH --network --publish H:C] run <command> [args...]")
		os.Exit(1)
	}

	cmd := flag.Arg(0)

	// Parse publish mappings
	var pubs []container.PortMapping
	for _, p := range flagPublishes {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "invalid --publish value %q, expected hostPort:containerPort\n", p)
			os.Exit(1)
		}
		hp, err1 := strconv.Atoi(parts[0])
		cp, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			fmt.Fprintf(os.Stderr, "invalid --publish ports in %q\n", p)
			os.Exit(1)
		}
		pubs = append(pubs, container.PortMapping{
			HostPort:      hp,
			ContainerPort: cp,
			Protocol:      "tcp",
		})
	}

	cfg := container.Config{
		Pids:       *flagPids,
		Memory:     *flagMemory,
		CPU:        *flagCPU,
		Rootfs:     *flagRootfs,
		Network:    *flagNetwork,
		BridgeCIDR: "10.0.0.0/24",
		Publishes:  pubs,
		HostIP:     hostIP,
	}

	switch cmd {
	case "run":
		if flag.NArg() < 2 {
			fmt.Println("Usage: main run <command> [args...]")
			os.Exit(1)
		}
		container.Run(cfg, flag.Args()[1:])

	case "child":
		container.Child(cfg, flag.Args()[1:])

	case "ps":
		container.Ps()

	case "stop":
		if flag.NArg() < 2 {
			fmt.Println("Usage: main stop <containerID>")
			os.Exit(1)
		}
		container.Stop(flag.Arg(1))

	case "exec":
		if flag.NArg() < 3 {
			fmt.Println("Usage: jcontainer exec <id> <command> [args...]")
			os.Exit(1)
		}
		id := flag.Arg(1)
		cmdArgs := flag.Args()[2:]
		container.Exec(id, cmdArgs)

	default:
		panic("Bad command. Use 'run' or 'child'")
	}
}
