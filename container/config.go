package container

type PortMapping struct {
	HostPort      int
	ContainerPort int
	Protocol      string
}

type Config struct {
	Pids       int
	Memory     string
	CPU        string
	Rootfs     string
	Network    bool
	BridgeCIDR string
	HostIP     string
	
	Publishes  []PortMapping
}
