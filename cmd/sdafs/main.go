package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"

	"github.com/pbnjay/memory"

	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/jacobsa/fuse"
	"github.com/sevlyar/go-daemon"
)

var credentialsFile, rootURL, logFile, extraCAFile string
var foreground, open bool
var maxRetries uint
var chunkSize uint
var cacheSize uint
var cacheMemPerCent uint
var logLevel int

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
	mountPoint string
	foreground bool
	logFile    string
	sdafsconf  *sdafs.Conf
	open       bool
	logLevel   slog.Level
}

// mainConfig makes the configuration structure from whatever sources applies
// (currently command line flags only)
func getConfigs() mainConfig {
	home := os.Getenv("HOME")

	credentialsDefault := fmt.Sprintf("%s/.s3cmd", home)
	flag.StringVar(&credentialsFile, "credentialsfile", credentialsDefault, "Credentials file")
	flag.StringVar(&rootURL, "rootURL", "https://download.bp.nbis.se", "Root URL for the SDA download interface")
	flag.StringVar(&logFile, "log", "", "File to send logs to instead of stderr,"+
		" defaults to sdafs.log if detached, empty string means stderr which is default for foreground")

	flag.StringVar(&extraCAFile, "extracafile", "", "File with extra CAs to regard (default no extra)")

	flag.UintVar(&maxRetries, "maxretries", 7, "Max number retries for failed transfers. "+
		"Retries will be done with some form of backoff. Max 60")
	flag.BoolVar(&foreground, "foreground", false, "Do not detach, run in foreground and send log output to stdout")
	flag.BoolVar(&open, "open", false, "Set permissions allowing access by others than the user")
	flag.UintVar(&chunkSize, "chunksize", 5120, "Chunk size (in kb) used when fetching data. "+
		"Higher values likely to give better throughput but higher latency. Min 64 Max 65536.")
	flag.IntVar(&logLevel, "loglevel", 0, "Loglevel, specified as per https://pkg.go.dev/log/slog#Level")
	flag.UintVar(&cacheSize, "cachesize", 0, "Cache size (in mb), overrides percent if set")
	flag.UintVar(&cacheMemPerCent, "cachemempercent", 8, "Cache size (in % of process visible RAM)")

	flag.Parse()

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

	conf := sdafs.Conf{
		RemoveSuffix:    true,
		RootURL:         rootURL,
		CredentialsFile: credentialsFile,
		SkipLevels:      0,
		ChunkSize:       uint64(chunkSize),
		MaxRetries:      int(maxRetries),
		ExtraCAFile:     extraCAFile,
	}

	if open {
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

	m := mainConfig{mountPoint: mountPoint,
		sdafsconf:  &conf,
		foreground: foreground,
		logFile:    useLogFile,
		open:       open,
		logLevel:   slog.Level(logLevel),
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

// detachIfNeeded daemonizes if needed
func detachIfNeeded(c mainConfig) {
	if !c.foreground {
		context := new(daemon.Context)
		child, err := context.Reborn()

		if err != nil {
			log.Fatalf("Failed to detach")
		}

		if child != nil {
			os.Exit(0)
		}

		if err := context.Release(); err != nil {
			slog.Info("Unable to release pid file",
				"error", err.Error())
		}
	}
}

func checkMountDir(m mainConfig) {
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
	mountConfig := &fuse.MountConfig{
		ReadOnly:                  true,
		FuseImpl:                  fuse.FUSEImplMacFUSE,
		DisableDefaultPermissions: true,
		FSName:                    fmt.Sprintf("SDA_%s", c.sdafsconf.RootURL),
		VolumeName:                fmt.Sprintf("SDA mount of %s", c.sdafsconf.RootURL),
	}

	if c.open {
		mountConfig.Options = make(map[string]string)
		mountConfig.Options["allow_other"] = ""
	}

	checkMountDir(c)

	fs, err := sdafs.NewSDAfs(c.sdafsconf)
	if err != nil {
		log.Fatalf("Error while creating sda fs: %v", err)
	}

	repointLog(c)
	detachIfNeeded(c)

	mount, err := fuse.Mount(c.mountPoint, fs.GetFileSystemServer(), mountConfig)
	if err != nil {
		log.Fatalf("Mount of sda at %s failed: %v", c.sdafsconf.RootURL, err)
	}
	slog.Info("SDA mount ready", "rootURL", c.sdafsconf.RootURL, "mountpoint", c.mountPoint)

	afterMount(c, mount)
	slog.Info("SDA unmounted", "mountpoint", c.mountPoint)
}

// afterMount does what happens after mount, essentially wait for a signal or
// for the file system to be unmounted
func afterMount(c mainConfig, mount *fuse.MountedFileSystem) {
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
		err := fuse.Unmount(m)
		if err != nil {
			slog.Error("Unmounting failed", "err", err)
			os.Exit(1)
		}
	}
}
