package daemon

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func WritePID(path string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(trimSpace(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file: %w", err)
	}
	return pid, nil
}

func RemovePID(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func IsRunning(path string) (bool, int, error) {
	pid, err := ReadPID(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, pid, nil
	}

	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		return false, pid, nil
	}
	return true, pid, nil
}

func Stop(path string) error {
	running, pid, err := IsRunning(path)
	if err != nil {
		return err
	}
	if !running {
		_ = RemovePID(path)
		return fmt.Errorf("streamd is not running")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}
	return nil
}

func Daemonize(logPath string) error {
	if os.Getppid() == 1 {
		return nil
	}

	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create pipe: %w", err)
	}

	pid, err := syscall.ForkExec(os.Args[0], os.Args, &syscall.ProcAttr{
		Files: []uintptr{os.Stdin.Fd(), w.Fd(), w.Fd()},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		return fmt.Errorf("fork: %w", err)
	}

	_ = w.Close()
	buf := make([]byte, 1)
	_, _ = r.Read(buf)
	_ = r.Close()

	_ = pid
	_ = logPath
	os.Exit(0)
	return nil
}

func NotifyParentReady() {
	// After fork, parent waits on pipe read; child closes stdout/stderr redirect.
}

func SignalNotifyContext() (chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	return ch, func() { signal.Stop(ch) }
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	return b
}
