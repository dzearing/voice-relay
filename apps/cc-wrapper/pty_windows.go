//go:build windows

package main

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/UserExistsError/conpty"
	"golang.org/x/term"
)

type winPTY struct {
	cpty *conpty.ConPty
}

func spawnPTY(args []string) (ptyHandle, error) {
	commandLine := strings.Join(args, " ")

	// Use actual terminal dimensions instead of hardcoded values
	width, height := 120, 30
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		width, height = w, h
	}

	cpty, err := conpty.Start(commandLine, conpty.ConPtyDimensions(width, height))
	if err != nil {
		return nil, err
	}

	return &winPTY{cpty: cpty}, nil
}

func (w *winPTY) Read(p []byte) (int, error) {
	n, err := w.cpty.Read(p)
	if err != nil {
		return n, io.EOF
	}
	return n, nil
}

func (w *winPTY) Write(p []byte) (int, error) {
	return w.cpty.Write(p)
}

func (w *winPTY) Close() error {
	return w.cpty.Close()
}

func (w *winPTY) Wait() int {
	exitCode, _ := w.cpty.Wait(context.Background())
	return int(exitCode)
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
