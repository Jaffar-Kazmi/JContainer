# JContainer

A minimal, educational container runtime written in Go that implements core containerization concepts using Linux kernel primitives: namespaces, cgroups, OverlayFS, veth/bridge networking, and iptables NAT.

## What is JContainer?

JContainer demonstrates how containers work at the OS level. It runs processes in isolated environments with their own:

    Process and hostname isolation (PID and UTS namespaces)

    Filesystem views (OverlayFS + chroot)

    Resource limits (cgroups: CPU, memory, process count)

    Network interfaces (veth pairs, bridge, port publishing)

## Architecture Overview

``` text
Host                              Container
─────────────────────────────────────────────
┌─ root user                    ┌─ PID 1 (init)
│                               │
├─ jcbr0 (bridge 10.0.0.1/24)   ├─ eth0 (10.0.0.X/24)
│                               │
├─ veth-host-ID ←→ veth-cont    │
│                               │
├─ cgroup /jcontainer-ID        ├─ in same cgroup
│                               │
├─ /tmp/jcontainer-ID/merged ←──┴─ chrooted here
│  (OverlayFS)                  │
│  ├─ lowerdir: /home/...       │
│  ├─ upperdir: /tmp/.../upper  │
│  └─ workdir:  /tmp/.../work   │
```
## Key components:

    Run/Child: Parent re-execs itself via /proc/self/exe to spawn into new namespaces.

    Cgroups: Limits pids, memory, CPU per container.

    OverlayFS: Read-only base rootfs + per-container writable layer.

    Networking: Bridge + veth + IPAM on 10.0.0.0/24 + optional iptables DNAT.

    Control plane: JSON state files under /run/jcontainer-state for ps/exec/stop.

Requirements

Host system:

    Linux (kernel 4.0+) with:

        User namespaces support (via kernel config)

        cgroups (v2)

        OverlayFS

        veth/bridge support

        iptables/netfilter

    Root privileges (or sudo)

    Go 1.22+

    Tools: ip, iptables, standard Unix utilities

Tested on: Arch Linux.

Not supported: Windows (WSL1/WSL2 has limited namespace support), macOS (no Linux kernel).

## Installation

Clone and Run:

``` bash
git clone https://github.com/Jaffar-Kazmi/JContainer.git
cd JContainer
sudo go run . [flags] run <command> [args]        # run without building
```

Build:

``` bash
sudo go build -o jcontainer .
sudo mv jcontainer /usr/local/bin/jcontainer
sudo jcontainer run bash                         # run using binary name
```
## Quick Start
1. Basic isolated shell

``` bash
sudo jcontainer run bash
```

Inside the container:

``` bash
hostname                    # jcontainer
ps                          # PID 1 is the container init
ls /                        # overlayfs-merged rootfs
id                          # still root (no user namespace)
exit
```

2. Networked container with port publishing

Ensure bridge exists (created on demand if missing):

```bash
sudo ip link show jcbr0  # should exist and be UP
```

Then:

``` bash
sudo jcontainer --network --publish 8080:80 run bash -c \
  "busybox httpd -f -p 80"
```
From host (in another terminal):

```bash
curl http://172.17.245.208:8080/  # Use actual host IP
# Should see BusyBox 404 or directory listing
```
3. Manage containers

### Start a background container:

``` bash
sudo jcontainer --network run bash -c "sleep 1000"
```
### List running containers:

```bash
sudo jcontainer ps
# ID       PID     IP          CMD
# 12345    12352   10.0.0.50   bash -c sleep 1000
```
### Enter the container:

```bash
sudo jcontainer exec 12345 bash
# Inside:
hostname                    # jcontainer
ip addr                     # shows eth0
exit
```
### Stop the container:

```bash
sudo jcontainer stop 12345
sudo jcontainer ps          # empty
```

### Flags:
  - --pids N              Max processes in container (default: 10)
  - --memory SIZE         Memory limit, e.g. 50M, 1G (default: 100M)
  - --cpu PCT             CPU limit as % of one core, e.g. 50% (default: 50%)
  - --rootfs PATH         Base rootfs directory (default: /home/jaffar/jroot)
  - --network             Enable container network namespace and veth
  - --publish H:C         Publish hostPort:containerPort (repeatable)

### Commands:
  - run COMMAND [ARGS]    Start a new container and attach
  - ps                    List running containers
  - exec ID COMMAND       Run command in container
  - stop ID               Terminate container by ID

### Setting Up Your Rootfs

The --rootfs flag points to a base filesystem that will become the container's read-only lower layer in OverlayFS.
#### Option A: Use the included jroot

If you cloned the repo and have /home/jaffar/jroot:

``` bash
sudo jcontainer run bash
```
(Default rootfs is /home/jaffar/jroot)

#### Option B: Create a minimal rootfs

Using debootstrap (Ubuntu/Debian):

```bash
mkdir -p ~/my-rootfs
sudo debootstrap --arch amd64 focal ~/my-rootfs
sudo jcontainer --rootfs ~/my-rootfs run bash
```
Using alpine:

```bash
mkdir -p ~/alpine-rootfs
sudo apk --root ~/alpine-rootfs --update-cache --allow-untrusted add -U apk-tools alpine-keys
sudo jcontainer --rootfs ~/alpine-rootfs run sh
```
#### Option C: Export from Docker

If you have Docker:

```bash
docker create --name temp-ubuntu ubuntu:20.04
docker export temp-ubuntu | tar -xf - -C ~/docker-rootfs
docker rm temp-ubuntu

sudo jcontainer --rootfs ~/docker-rootfs run bash
```
Key points:

    --rootfs must contain /bin, /etc, /lib, etc. (or whatever your container needs).

    It is read-only during container execution (OverlayFS lowerdir).

    Per-container writable changes go to /tmp/jcontainer-<id>/upper.

    Must be readable by root.

### Networking Details
#### Bridge setup

When you use --network, jcontainer:

    Creates (if missing) a Linux bridge jcbr0 with IP 10.0.0.1/24.

    Creates a veth pair for each container:

        Host side (veth-host-<id>) → attached to jcbr0

        Container side (veth-cont-<id>) → moved to container netns, renamed eth0

    Assigns container IP from 10.0.0.0/24 (derived from container PID).

    Enables outbound NAT via iptables MASQUERADE.

#### Port publishing

--publish 8080:80 adds iptables DNAT rules:

```bash
# Incoming traffic to host:8080 → container IP:80
iptables -t nat -A PREROUTING -p tcp --dport 8080 \
  -j DNAT --to-destination 10.0.0.X:80

# Local traffic to host IP:8080 → container IP:80
iptables -t nat -A OUTPUT -p tcp -d HOST_IP --dport 8080 \
  -j DNAT --to-destination 10.0.0.X:80
```
Host IP detection: Automatically finds your primary non-loopback IPv4 (usually your Wi-Fi or Ethernet IP). If detection fails, --publish is skipped with a warning.
#### Testing connectivity

From host:

```bash
sudo jcontainer --network run bash -c "sleep 100"
# In another terminal:
sudo jcontainer ps                          # note container ID and IP
ping 10.0.0.X                              # direct to container IP
curl http://10.0.0.X:80/                  # if service is running
```
From container:

```bash
ip addr                                    # see eth0
ping -c1 10.0.0.1                         # reach bridge
ping -c1 8.8.8.8                          # reach internet (if NAT works)
```
Exec Behavior

jcontainer exec <id> md> joins the container's UTS, mount, and net namespaces and chroots into its rootfs, then runs the command.

Current limitations:

    Does not fully join the PID namespace, so ps shows host PID numbers for the exec session. The original container (started with run) still shows container-local PIDs (1, 2, ...).

    /proc is mounted fresh inside each exec session.

Example:

```bash
# Terminal 1
sudo jcontainer --network run bash -c "sleep 1000"

# Terminal 2
sudo jcontainer exec <id> ps
# Shows: sudo, go, JContainer, ps with host PID numbers

# But inside the bash from Terminal 1:
ps
# Shows: PID 1 (init), bash, ps with container-local numbering
```
This is intentional (avoids Go's threading complexity with PID namespace joins) and doesn't break functionality.
### State and Cleanup
#### State directory

Running containers are tracked in /run/jcontainer-state/<id>.json:

```json
{
  "id": "12345",
  "init_pid": 12352,
  "ip": "10.0.0.50",
  "created_at": "2025-12-06T...",
  "command": ["bash", "-c", "sleep 1000"],
  "merged_dir": "/tmp/jcontainer-12345/merged"
}
```

ps reads these files and checks /proc/<pid> to filter out dead containers.
#### Cleanup

stop or normal exit cleans up:

    Veth pairs under /sys/class/net

    Cgroups under /sys/fs/cgroup/jcontainer-<id>

    OverlayFS layers under /tmp/jcontainer-<id>/

    State file /run/jcontainer-state/<id>.json

    iptables rules for that container

If the host crashes, stale /tmp/jcontainer-* dirs and cgroups may remain. Clean manually:

bash
sudo rm -rf /tmp/jcontainer-*
sudo rmdir /sys/fs/cgroup/jcontainer-* 2>/dev/null || true

## Limitations

    Root-only: Requires sudo for all operations (cgroups, namespaces, mounts, iptables).

    No user namespace: Containers run as root on the host (uid 0 inside, uid 0 outside).

    No seccomp/capabilities: No syscall filtering or capability dropping.

    No image/registry: --rootfs is your "image"—point to a prebuilt rootfs directory.

    Simplified exec: PID namespace not fully joined; host PIDs visible in exec sessions.

## References & Further Reading

    Linux namespaces and container isolation: LWN.net Namespace articles

    OverlayFS usage: kernel.org overlayfs docs

    iptables port publishing: Docker: How to publish ports

    cgroups: kernel.org cgroups v2 docs

## License

Educational project. MIT.
