package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
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
	sync           bool
)

func init() {
	flag.StringVar(&repoDir, "repo", repoDir, "The directory in which to store the downloaded media")
	flag.BoolVar(&keepEverything, "everything", keepEverything, "Whether to store all metadata returned by API for each item")
	flag.StringVar(&logFile, "log", logFile, "Write logs to a file, stdout, or stderr")
	flag.StringVar(&every, "every", every, "How often to run this command, blocking indefinitely")
	flag.IntVar(&concurrency, "concurrency", concurrency, "How many downloads to do in parallel")
	flag.BoolVar(&sync, "sync", sync, "Download new items then perform a destructive sync")
}

func main() {
	flag.Parse()

	if concurrency < 1 {
		log.Fatal("concurrency must be at least 1")
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
	repo, err := photobak.OpenRepo(repoDir)
	if err != nil {
		return fmt.Errorf("opening repo: %v", err)
	}
	defer repo.Close()

	// TODO: Close the repo cleanly if SIGINT is received

	repo.NumWorkers = concurrency

	err = repo.StoreAll(keepEverything)
	if err != nil {
		return err
	}

	if sync {
		err := repo.Sync()
		if err != nil {
			return err
		}
	}

	return nil
}
