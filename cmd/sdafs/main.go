package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/NBISweden/sdafs/internal/cgofuseadapter"
	"github.com/pbnjay/memory"

	"github.com/NBISweden/sdafs/internal/sdafs"
)

var Version string = "development"

// usage prints usage and version for the benefit of the user
func usage() {
	_, err := fmt.Fprintf(flag.CommandLine.Output(),
		"Usage: %s [FLAGS...] mountpoint\n\nSupported flags are:\n\n",
		os.Args[0])
	if err != nil {
		panic("the world is unreliable, we can't go on")
	}
	flag.PrintDefaults()
	fmt.Printf("\nsdafs version: %s\n\n", Version)
	os.Exit(1)
}

// mainConfig holds the configuration
type mainConfig struct {
	mountPoint     string
	foreground     bool
	logFile        string
	sdafsconf      *sdafs.Conf
	open           bool
	logLevel       slog.Level
	cgofuse        bool
	cgofuseOptions string
}

// mainConfig makes the configuration structure from whatever sources applies
// (currently command line flags only)
func getConfigs() mainConfig {
	var credentialsFile, rootURL, logFile, extraCAFile, datasets string
	var foreground, open bool
	var maxRetries uint
	var chunkSize uint
	var cacheSize uint
	var cacheMemPerCent uint
	var cacheMaxTTL time.Duration
	var logLevel int
	var owner uint
	var group uint
	var cgofuse bool
	var cgofuseOptions string

	var credentialsDefault string
	home := os.UserHomeDir()

	if len(home) > 0 {
		credentialsDefault = filepath.Join(home, ".s3cfg")
	} else {
		credentialsDefault = ".s3cfg"
	}

	flag.StringVar(&credentialsFile, "credentialsfile", credentialsDefault, "Credentials file")
	flag.StringVar(&rootURL, "rootURL", "https://download.bp.nbis.se", "Root URL for the SDA download interface")
	flag.StringVar(&logFile, "log", "", "File to send logs to instead of stderr,"+
		" defaults to sdafs.log if detached, empty string means stderr which is default for foreground")

	flag.StringVar(&extraCAFile, "extracafile", "", "File with extra CAs to regard (default no extra)")

	flag.StringVar(&datasets, "datasets", "", "Only expose listed datasets (comma separated list, default all)")

	flag.UintVar(&maxRetries, "maxretries", 7, "Max number retries for failed transfers. "+
		"Retries will be done with some form of backoff. Max 60")

	flag.UintVar(&chunkSize, "chunksize", 5120, "Chunk size (in kb) used when fetching data. "+
		"Higher values likely to give better throughput but higher latency. Min 64 Max 65536.")
	flag.IntVar(&logLevel, "loglevel", 0, "Loglevel, specified as per https://pkg.go.dev/log/slog#Level")
	flag.UintVar(&cacheSize, "cachesize", 0, "Cache size (in mb), overrides percent if set")
	flag.UintVar(&cacheMemPerCent, "cachemempercent", 8, "Cache size (in % of process visible RAM)")
	flag.DurationVar(&cacheMaxTTL, "cachettl", 0, "Maximum time to live for cache entries (e.g. '2h', '4m30s'). Default 0 means no ttl expiry")

	flag.UintVar(&owner, "owner", 0, "Numeric uid to use as entity owner rather than current uid")
	flag.UintVar(&group, "group", 0, "Numeric gid to use as entity group rather than current gid")

	// foregound mandatory on Windows for now
	if runtime.GOOS != "windows" {
		flag.BoolVar(&foreground, "foreground", false, "Do not detach, run in foreground and send log output to stdout")
	} else {
		foreground = true
	}

	// cgofuse mandatory on Windows, may be an option for some platforms
	if runtime.GOOS != "windows" && cgofuseadapter.CGOFuseAvailable() {
		flag.BoolVar(&cgofuse, "usecgofuse", false, "Use alternate fuse layer for wider availability (implies open)")
		flag.StringVar(&cgofuseOptions, "cgofuseoptions", "", "Options passed to cgofuse mount")
	}

	if runtime != "windows" && !cgofuseadapter.CGOFuseAvailable() {
		cgofuse = false
	}

	if runtime.GOOS == "windows" {
		open = true
		cgofuse = true
		flag.StringVar(&cgofuseOptions, "cgofuseoptions", "", "Options passed to cgofuse mount")
	} else {
		// Platforms other than Windows doesn't have open mandatory
		flag.BoolVar(&open, "open", false, "Set permissions allowing access by others than the user")
	}

	flag.Parse()

	passed := make([]string, 0)

	flag.Visit(func(f *flag.Flag) {
		passed = append(passed, f.Name)
	})

	mountPoint := flag.Arg(0)
	if mountPoint == "" || len(flag.Args()) != 1 {
		usage()
	}

	// Some sanity checks
	if chunkSize > 65536 || chunkSize < 64 {
		fmt.Printf("Chunk size %d is not allowed, valid values are 64 to 16384\n\n",
			chunkSize)
		usage()
	}

	if maxRetries > 60 {
		fmt.Printf("Max retries %d is not allowed, valid values are 0 to 60\n\n	",
			maxRetries)
		usage()
	}

	if len(extraCAFile) > 0 {
		testOpen, err := os.Open(extraCAFile)
		if err != nil {
			log.Fatalf("Error while opening requested extra CA file %s: %v",
				extraCAFile,
				err)
		}

		defer testOpen.Close() // nolint:errcheck
	}

	useLogFile := logFile
	// Background and logfile not specified
	if !foreground && logFile == "" {
		useLogFile = "sdafs.log"
	}

	var showDatasets []string
	if strings.TrimSpace(datasets) != "" {
		showDatasets = strings.Split(datasets, ",")

		for i := range showDatasets {
			showDatasets[i] = strings.TrimSpace(showDatasets[i])
		}
	}

	conf := sdafs.Conf{
		RemoveSuffix:    true,
		RootURL:         rootURL,
		CredentialsFile: credentialsFile,
		SkipLevels:      0,
		ChunkSize:       uint64(chunkSize),
		MaxRetries:      int(maxRetries),
		ExtraCAFile:     extraCAFile,
		DatasetsToShow:  showDatasets,
		CacheMaxTTL:     cacheMaxTTL,
	}

	if slices.Contains(passed, "owner") {

		if owner > uint(^uint32(0)) {
			log.Fatalf("uid requested larger than allowed %d", ^uint32(0))
		}

		conf.SpecifyUID = true
		conf.UID = uint32(owner)
	}

	if slices.Contains(passed, "group") {

		if group > uint(^uint32(0)) {
			log.Fatalf("gid requested larger than allowed %d", ^uint32(0))
		}

		conf.SpecifyGID = true
		conf.GID = uint32(group)
	}

	if cgofuse || open {
		conf.SpecifyDirPerms = true
		conf.SpecifyFilePerms = true

		conf.DirPerms = 0555
		conf.FilePerms = 0444
	}

	if cacheSize == 0 {
		total := memory.TotalMemory()
		conf.CacheSize = total * uint64(cacheMemPerCent) / 100
	} else {
		conf.CacheSize = uint64(cacheSize * 1024 * 1024)
	}

	if runtime.GOOS == "windows" {
		_, err := os.Stat(mountPoint)
		if err == nil {
			log.Fatalf("on Windows, mountpoint must not exist")
		}

		if !errors.Is(err, fs.ErrNotExist) {
			log.Fatalf("unexpected error when checking mountpoint %s: %v",
				mountPoint,
				err)
		}
	}

	m := mainConfig{mountPoint: mountPoint,
		sdafsconf:      &conf,
		foreground:     foreground,
		logFile:        useLogFile,
		open:           open,
		logLevel:       slog.Level(logLevel),
		cgofuse:        cgofuse,
		cgofuseOptions: cgofuseOptions,
	}

	return m
}

// repointLog switches where the log goes if needed
func repointLog(m mainConfig) {

	var logDestination io.Writer = os.Stdout

	if m.logFile != "" {
		var err error
		logDestination, err = os.OpenFile(m.logFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)

		if err != nil {
			log.Fatalf("Couldn't open requested log file %s: %v",
				m.logFile, err)
		}
	}
	options := slog.HandlerOptions{Level: m.logLevel}

	handler := slog.NewTextHandler(logDestination, &options)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

func checkMountDir(m mainConfig) {
	if runtime.GOOS == "windows" {
		return
	}

	st, err := os.Stat(m.mountPoint)

	if err != nil {
		log.Fatalf("Error while checking desired mount point %s: %v",
			m.mountPoint, err)
	}

	if !st.IsDir() {
		log.Fatalf("Error while checking mount point %s: not a directory", m.mountPoint)
	}
}

func main() {
	c := getConfigs()

	checkMountDir(c)

	fs, err := sdafs.NewSDAfs(c.sdafsconf)
	if err != nil {
		log.Fatalf("Error while creating sda fs: %v", err)
	}

	repointLog(c)
	detachIfNeeded(c)

	mount, err := doMount(c, fs)

	if err != nil {
		log.Fatalf("Mount of sda at %s failed: %v", c.sdafsconf.RootURL, err)
	}

	afterMount(c, mount)
	slog.Info("SDA unmounted", "mountpoint", c.mountPoint)
}

// afterMount does what happens after mount, essentially wait for a signal or
// for the file system to be unmounted
func afterMount(c mainConfig, mount cgofuseadapter.MountedFS) {
	ch := make(chan os.Signal, 1)
	go handleSignals(ch, c.mountPoint)
	signal.Notify(ch, os.Interrupt)
	// signal.Notify(c, os.Ter)

	err := mount.Join(context.Background())
	if err != nil {
		log.Fatalf("Error while waiting for mount: %v", err)
	}
}

// Signal handler for interrupt, should try to unmount the filesystem
func handleSignals(c chan os.Signal, m string) {
	for {
		s := <-c

		slog.Debug("Received signal exiting", "signal", s)
		// TODO: Retry on failure?
		err := doUnmount(m)
		if err != nil {
			slog.Error("Unmounting failed", "err", err)
			os.Exit(1)
		}
	}
}
