package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/llehouerou/gosoulseek"

	log "github.com/sirupsen/logrus"
)

func main() {
	ctx := context.Background()
	log.SetFormatter(&log.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: "02-01-2006 15:04:05",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := path.Base(f.File)
			return fmt.Sprintf("%s()", f.Function), fmt.Sprintf("%s:%d", filename, f.Line)
		},
	})
	log.SetLevel(log.DebugLevel)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter username ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimRight(username, "\n")
	fmt.Print("Enter password ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimRight(password, "\n")
	client := gosoulseek.NewClient("server.slsknet.org", 2242)
	err := client.Connect(ctx)
	if err != nil {
		panic(err)
	}

	err = client.Login(ctx, username, password)
	if err != nil {
		panic(err)
	}
	client.Search("2020 fleet foxes")
	for {
		select {}
	}

}
