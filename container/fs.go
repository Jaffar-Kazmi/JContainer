package container

import (
	"fmt"
	"os"
	"syscall"
)

func settupOverlayFS(containerID string, cfg Config) (string, error) {
	fmt.Println("DEBUG: flagRootfs =", cfg.Rootfs)
	baseDir := cfg.Rootfs
	lowerDir := baseDir

	info, err := os.Stat(lowerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("lowerdir %s does not exist", lowerDir)
		}
		return "", fmt.Errorf("error checking lowerdir %s: %v", lowerDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("lowerdir %s is not a directory", lowerDir)
	}

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
