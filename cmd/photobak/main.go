package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/mholt/photobak"
	_ "github.com/mholt/photobak/googlephotos"
)

var (
	repoDir        = "./photos_backup"
	keepEverything = false
	checkIntegrity = false
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
	flag.BoolVar(&checkIntegrity, "integrity", checkIntegrity, "Enable integrity checks for items that already exist in the database")
	flag.StringVar(&logFile, "log", logFile, "Write logs to a file, stdout, or stderr")
	flag.StringVar(&every, "every", every, "How often to run this command, blocking indefinitely")
	flag.IntVar(&concurrency, "concurrency", concurrency, "How many downloads to do in parallel")
	flag.BoolVar(&prune, "prune", prune, "Clean up removed photos and albums")
	flag.BoolVar(&authOnly, "authonly", authOnly, "Obtain authorizations only; do not perform backups")
	flag.BoolVar(&verbose, "v", verbose, "Write informational log messages to stdout")
}

type daemon struct {
	repo       *photobak.Repository
	repoMu     sync.Mutex
	signalChan chan os.Signal
}

func startDaemon(interval time.Duration) {
	if runtime.GOOS != "windows" {
		// The default behaviour on SIGPIPE is to silently terminate the program which breaks clean shutdown, so ignore
		// it because every program should check write() return code instead of crashing if some file descriptor became
		// unavailable for writing.
		signal.Notify(make(chan os.Signal), syscall.SIGPIPE)
	}

	d := daemon{signalChan: make(chan os.Signal, 1)}
	signal.Notify(d.signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-d.signalChan
		log.Println("[INTERRUPT] Closing database and quitting")
		d.close(true)
	}()

	if err := d.run(); err != nil {
		if interval == 0 {
			log.Fatal(err)
		} else {
			log.Println(err)
		}
	}

	if interval == 0 {
		return
	}

	for range time.Tick(interval) {
		log.Println("Running backup")
		if err := d.run(); err != nil {
			log.Println(err)
		}
	}
}

func (d *daemon) run() error {
	repo, err := photobak.OpenRepo(repoDir)
	if err != nil {
		return fmt.Errorf("opening repo: %v", err)
	}

	d.repoMu.Lock()
	d.repo = repo
	d.repoMu.Unlock()
	defer d.close(false)

	repo.NumWorkers = concurrency

	if prune {
		return repo.Prune()
	}

	return repo.Store(keepEverything, checkIntegrity)
}

func (d *daemon) close(exit bool) {
	d.repoMu.Lock()
	defer d.repoMu.Unlock()

	if d.repo != nil {
		if exit {
			d.repo.CloseUnsafeOnExit()
		} else {
			d.repo.Close()
		}
		d.repo = nil
	}

	if exit {
		os.Exit(0)
	}
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

	startDaemon(itvl)
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
