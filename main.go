package main

import (
	// "flag"
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
)

func main() {
	var err error
	var f *os.File
	var name string
	var buf bytes.Buffer

	name = os.Args[1]
	// flag.StringVar(&name, "name", "", "arg for func name")
	// flag.Parse()

	cmd := exec.Command("go", "run", "single/"+name+".go")
	// 同时写到 buffer（用于后续记录）和控制台
	cmd.Stdout = io.MultiWriter(&buf, os.Stdout)
	// 把 stderr 也写到同一个 buf（或单独 errBuf）并打印到控制台
	cmd.Stderr = io.MultiWriter(&buf, os.Stderr)
	cmd.Stdin = os.Stdin

	if err = cmd.Run(); err != nil {
		panic(err)
	}

	f, err = os.OpenFile("single.log", os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	lg := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	lg.Debug(buf.String())
}
