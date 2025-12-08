package container

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const stateDir = "/run/jcontainer-state"

type ContainerState struct {
    ID        string    `json:"id"`
    InitPID   int       `json:"init_pid"`
    IP        string    `json:"ip"`
    CreatedAt time.Time `json:"created_at"`
    Command   []string  `json:"command"`
    MergedDir string    `json:"merged_dir"`
}

func writeState(st *ContainerState) error {
    if err := os.MkdirAll(stateDir, 0755); err != nil {
        return err
    }
    path := filepath.Join(stateDir, st.ID+".json")
    tmp := path + ".tmp"

    data, err := json.MarshalIndent(st, "", "  ")
    if err != nil {
        return err
    }
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}

func Ps() {
    entries, err := os.ReadDir(stateDir)
    if err != nil {
        fmt.Println("No containers found")
        return
    }

    fmt.Printf("ID       PID     IP           CMD\n")
    for _, e := range entries {
        if !strings.HasSuffix(e.Name(), ".json") {
            continue
        }
        data, err := os.ReadFile(filepath.Join(stateDir, e.Name()))
        if err != nil {
            continue
        }

        var st ContainerState
        if err := json.Unmarshal(data, &st); err != nil {
            continue
        }

        // Skip dead processes
        if _, err := os.Stat(fmt.Sprintf("/proc/%d", st.InitPID)); err != nil {
            continue
        }

        fmt.Printf("%-7s %-7d %-12s %s\n",
            st.ID, st.InitPID, st.IP, strings.Join(st.Command, " "))
    }
}


func loadState(id string) (*ContainerState, error) {
    path := filepath.Join(stateDir, id+".json")
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var st ContainerState
    if err := json.Unmarshal(data, &st); err != nil {
        return nil, err
    }
    return &st, nil
}

func Stop(id string) {
    st, err := loadState(id)
    if err != nil {
        fmt.Printf("no such container: %s\n", id)
        return
    }

    // 1) Try SIGTERM
    _ = syscall.Kill(st.InitPID, syscall.SIGTERM)

    // 2) Wait a bit
    time.Sleep(2 * time.Second)
    if _, err := os.Stat(fmt.Sprintf("/proc/%d", st.InitPID)); err == nil {
        // Still alive → SIGKILL
        _ = syscall.Kill(st.InitPID, syscall.SIGKILL)
    }

    // 3) Cleanup like Run’s tail (reuse helpers)
    cleanupVeth(st.ID)
    cleanupCgroup(fmt.Sprintf("/sys/fs/cgroup/jcontainer-%s", st.ID))
    cleanupOverlay(st.ID)
    os.Remove(filepath.Join(stateDir, st.ID+".json"))
}

func Exec(id string, cmdArgs []string) {
    st, err := loadState(id)
    if err != nil {
        fmt.Printf("no such container: %s\n", id)
        return
    }
    if len(cmdArgs) == 0 {
        fmt.Println("exec: missing command")
        return
    }

    // 1) Join UTS + NET namespaces
    nsList := []string{"uts", "net"}
    for _, n := range nsList {
        fd, err := os.Open(fmt.Sprintf("/proc/%d/ns/%s", st.InitPID, n))
        if err != nil {
            fmt.Printf("open ns %s: %v\n", n, err)
            return
        }
        if err := unix.Setns(int(fd.Fd()), 0); err != nil {
            fmt.Printf("setns %s: %v\n", n, err)
            fd.Close()
            return
        }
        fd.Close()
    }

    // 2) chroot + chdir into container root
    if err := syscall.Chroot(st.MergedDir); err != nil {
        fmt.Printf("chroot: %v\n", err)
        return
    }
    if err := syscall.Chdir("/"); err != nil {
        fmt.Printf("chdir: %v\n", err)
        return
    }

    // 3) Mount /proc so tools like ps work
    if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
        fmt.Printf("mount /proc: %v\n", err)
        return
    }
    defer syscall.Unmount("/proc", 0)

    // 4) Run the command using exec.Command so PATH is honored
    cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        fmt.Printf("exec: %v\n", err)
    }
}


