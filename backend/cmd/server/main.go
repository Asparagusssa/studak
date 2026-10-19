package main

import (
	"os"

	"studak/internal/app"
	"studak/internal/logx"
)

func main() {
	if err := app.Run(); err != nil {
		logx.Error("application stopped", err)
		os.Exit(1)
	}
}
