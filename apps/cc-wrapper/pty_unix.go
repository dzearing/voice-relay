//go:build !windows

package main

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

type unixPTY struct {
	ptmx *os.File
	cmd  *exec.Cmd
}

func spawnPTY(args []string) (ptyHandle, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	// Inherit terminal size
	if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
		pty.Setsize(ptmx, sz)
	}

	// Forward SIGWINCH for terminal resize
	go handleResize(ptmx)

	return &unixPTY{ptmx: ptmx, cmd: cmd}, nil
}

func handleResize(ptmx *os.File) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	for range ch {
		if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
			pty.Setsize(ptmx, sz)
		}
	}
}

func (u *unixPTY) Read(p []byte) (int, error) {
	n, err := u.ptmx.Read(p)
	if err != nil {
		return n, io.EOF
	}
	return n, nil
}

func (u *unixPTY) Write(p []byte) (int, error) {
	return u.ptmx.Write(p)
}

func (u *unixPTY) Close() error {
	return u.ptmx.Close()
}

func (u *unixPTY) Wait() int {
	err := u.cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
		return 1
	}
	return 0
}

func setRawTerminal() (func(), error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() {
		term.Restore(fd, oldState)
	}, nil
}
