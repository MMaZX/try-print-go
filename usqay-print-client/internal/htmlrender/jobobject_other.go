//go:build !windows

package htmlrender

import "os/exec"

// attachToJobObject is a Windows-only mitigation (orphaned Chrome child
// processes are not a concern on Linux, where allocateCmdOptions already
// sets Pdeathsig — see chromedp's allocate_linux.go). Never called on this
// platform; render.go only wires it in when runtime.GOOS == "windows".
func attachToJobObject(cmd *exec.Cmd, result chan<- uintptr) {}

// closeJobHandle is a no-op outside Windows; h is always 0 there.
func closeJobHandle(h uintptr) {}
