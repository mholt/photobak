package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/mholt/photobak"
	_ "github.com/mholt/photobak/googlephotos"
)

var (
	repoDir        = "./photos_backup"
	keepEverything = false
	logFile        = "stderr"
	concurrency    = 5
	every          string
	prune          bool
	authOnly       bool
	verbose        bool
)

func init() {
	flag.StringVar(&repoDir, "repo", repoDir, "The directory in which to store the downloaded media")
	flag.BoolVar(&keepEverything, "everything", keepEverything, "Whether to store all metadata returned by API for each item")
	flag.StringVar(&logFile, "log", logFile, "Write logs to a file, stdout, or stderr")
	flag.StringVar(&every, "every", every, "How often to run this command, blocking indefinitely")
	flag.IntVar(&concurrency, "concurrency", concurrency, "How many downloads to do in parallel")
	flag.BoolVar(&prune, "prune", prune, "Clean up removed photos and albums")
	flag.BoolVar(&authOnly, "authonly", authOnly, "Obtain authorizations only; do not perform backups")
	flag.BoolVar(&verbose, "v", verbose, "Write informational log messages to stdout")
}

func main() {
	flag.Parse()

	if verbose {
		photobak.Info = log.New(os.Stdout, "", log.LstdFlags)
	}

	switch logFile {
	case "stdout":
		log.SetOutput(os.Stdout)
	case "stderr":
		log.SetOutput(os.Stderr)
	case "":
		log.SetOutput(ioutil.Discard)
	default:
		log.SetOutput(&lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    100,
			MaxAge:     90,
			MaxBackups: 10,
		})
	}

	if concurrency < 1 {
		log.Fatal("concurrency must be at least 1")
	}

	if authOnly {
		err := authorize()
		if err != nil {
			log.Fatalf("[ERROR] %v", err)
		}
		fmt.Println("All configured accounts have credentials.")
		return
	}

	// parse the interval, if present, right away
	// so we can report error immediately if needed.
	var itvl time.Duration
	if every != "" {
		var err error
		itvl, err = parseEvery(every)
		if err != nil {
			log.Fatal(err)
		}
	}

	err := run()
	if err != nil {
		log.Fatal(err)
	}

	if every != "" {
		c := time.Tick(itvl)
		for range c {
			log.Println("Running backup")
			err := run()
			if err != nil {
				log.Println(err)
			}
		}
	}
}

func parseEvery(every string) (time.Duration, error) {
	if len(every) == 0 {
		return 0, fmt.Errorf("no interval given")
	}

	num, unit := every[:len(every)-1], every[len(every)-1:]

	minutes, err := strconv.Atoi(num)
	if err != nil {
		return 0, fmt.Errorf("bad interval value: %v", err)
	}
	if minutes < 1 {
		return 0, fmt.Errorf("interval %d is less than 1", minutes)
	}

	switch unit {
	case "h":
		minutes *= 60
	case "d":
		minutes *= 60 * 24
	case "m":
		// already in minutes
	default:
		return 0, fmt.Errorf("unknown unit '%s': must be m, h, or d", unit)
	}

	return time.Duration(minutes) * time.Minute, nil
}

func run() error {
	waitchan := make(chan struct{})

	repo, err := photobak.OpenRepo(repoDir)
	if err != nil {
		return fmt.Errorf("opening repo: %v", err)
	}
	defer close(waitchan)

	// cleanly close repository when interrupted
	// or when the function ends
	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, os.Interrupt)
		select {
		case <-waitchan:
			repo.Close()
		case <-sigchan:
			log.Println("[INTERRUPT] Closing database and quitting")
			repo.Close()
			os.Exit(0)
		}
	}()

	repo.NumWorkers = concurrency

	if prune {
		return repo.Prune()
	}

	return repo.Store(keepEverything)
}

func authorize() error {
	fmt.Println("[Authorization Mode]")
	fmt.Println("No backups will be performed, but credentials will be obtained")
	fmt.Println("and stored to the database in the repo. You may then use this")
	fmt.Printf("repository headless.\n\n")

	repo, err := photobak.OpenRepo(repoDir)
	if err != nil {
		return fmt.Errorf("opening repository: %v", err)
	}
	defer repo.Close()

	return repo.AuthorizeAllAccounts()
}
