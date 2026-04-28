package main

import (
	"os"

	"github.com/Votline/Go-audio/pkg/audio"
)

func main() {
	acl, err := audio.Init(0, 0, 0, 0, 0, 0, 0, 0, true, nil)
	if err != nil {
		panic(err)
	}

	file, err := os.OpenFile("test.test", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	mode := os.Args[1]
	switch mode {
	case "record":
		if err := acl.Record(file); err != nil {
			panic(err)
		}

	case "play":
		if err := acl.Play(file); err != nil {
			panic(err)
		}
	default:
		panic("unknown mode")
	}
}
