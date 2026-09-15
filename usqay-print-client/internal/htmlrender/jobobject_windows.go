//go:build windows

package htmlrender

import (
	"log/slog"
	"os"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// attachToJobObject makes Windows kill every descendant process of cmd (GPU,
// renderer and utility processes — Chrome does not group these into a single
// process tree the way it does on Linux) the moment the Job Object's last
// handle is closed, however cmd's own process dies (graceful close, crash,
// or a hard Kill triggered by exec.CommandContext when the engine's context
// is cancelled).
//
// Without this, a persistent engine that gets recreated after a crash (see
// Engine.isHealthy / newEngine) leaves its old renderer processes running as
// orphans: they are never grouped with, or torn down alongside, the parent
// chrome.exe. Over long uptimes these orphans accumulate until Windows can no
// longer start a new Chrome process at all ("chrome failed to start").
//
// The resulting Job handle (or 0 on any failure) is sent once on result —
// the caller (render.go's newEngine) stores it on Engine.jobHandle and closes
// it explicitly in Engine.Close(), so each engine recycle also sweeps up any
// straggler from that generation instead of relying on the whole client
// process exiting.
func attachToJobObject(cmd *exec.Cmd, result chan<- uintptr) {
	go func() {
		proc := waitForProcess(cmd)
		if proc == nil {
			slog.Warn("no se pudo obtener el proceso de Chromium para asignarlo al Job Object", "src", "CHROMIUM")
			result <- 0
			return
		}

		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			slog.Warn("no se pudo crear Job Object para Chromium", "error", err, "src", "CHROMIUM")
			result <- 0
			return
		}

		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			job,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			slog.Warn("no se pudo configurar Job Object para Chromium", "error", err, "src", "CHROMIUM")
			windows.CloseHandle(job)
			result <- 0
			return
		}

		hProcess, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(proc.Pid))
		if err != nil {
			slog.Warn("no se pudo abrir el proceso de Chromium para asignarlo al Job Object", "error", err, "src", "CHROMIUM")
			windows.CloseHandle(job)
			result <- 0
			return
		}
		defer windows.CloseHandle(hProcess)

		if err := windows.AssignProcessToJobObject(job, hProcess); err != nil {
			slog.Warn("no se pudo asignar Chromium al Job Object", "error", err, "src", "CHROMIUM")
			windows.CloseHandle(job)
			result <- 0
			return
		}

		slog.Debug("Chromium asignado a Job Object (evita procesos huerfanos al morir)", "pid", proc.Pid, "src", "CHROMIUM")
		result <- uintptr(job)
	}()
}

// waitForProcess polls for cmd.Process, which chromedp sets synchronously
// inside cmd.Start() — called right after invoking the ModifyCmdFunc hook
// this function runs from, on the same goroutine, so a short poll is enough.
// chromedp does not expose any callback for "right after Start()", so
// polling is the only option here.
func waitForProcess(cmd *exec.Cmd) *os.Process {
	for i := 0; i < 200; i++ { // hasta ~1s
		if cmd.Process != nil {
			return cmd.Process
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// closeJobHandle closes the Windows Job Object handle, which (because of
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE) immediately terminates any process
// still assigned to it. h is 0 when no Job Object was ever assigned.
func closeJobHandle(h uintptr) {
	if h == 0 {
		return
	}
	windows.CloseHandle(windows.Handle(h))
}
