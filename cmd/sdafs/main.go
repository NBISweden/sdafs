package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/jacobsa/fuse"
	"github.com/sevlyar/go-daemon"
)

var credentialsFile, rootURL, logFile string
var foreground, open bool

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(),
		"Usage: %s [FLAGS...] mountpoint\n\nSupported flags are:\n\n",
		os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

type mainConfig struct {
	mountPoint string
	foreground bool
	logFile    string
	sdafsconf  *sdafs.Conf
}

func getConfigs() mainConfig {
	home := os.Getenv("HOME")

	credentialsDefault := fmt.Sprintf("%s/.s3cmd", home)
	flag.StringVar(&credentialsFile, "credentialsfile", credentialsDefault, "Credentials file")
	flag.StringVar(&rootURL, "rootURL", "https://download.bp.nbis.se", "Root URL for the SDA download interface")
	flag.StringVar(&logFile, "log", "", "File to send logs to instead of stderr,"+
		" defaults to sdafs.log if detached, empty string means stderr which is default for foreground")

	flag.BoolVar(&foreground, "foreground", false, "Do not detach")
	flag.BoolVar(&open, "open", false, "Set permissions allowing access by others than the user")

	flag.Parse()

	mountPoint := flag.Arg(0)
	if mountPoint == "" || len(flag.Args()) != 1 {
		usage()
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
	}

	if open {
		conf.SpecifyDirPerms = true
		conf.SpecifyFilePerms = true

		conf.DirPerms = 0555
		conf.FilePerms = 0444
	}

	m := mainConfig{mountPoint: mountPoint,
		sdafsconf:  &conf,
		foreground: foreground,
		logFile:    useLogFile,
	}

	return m
}

func repointLog(m mainConfig) {
	if m.logFile != "" {
		f, err := os.OpenFile(m.logFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)

		if err != nil {
			log.Fatalf("Couldn't open requested log file %s: %v",
				m.logFile, err)
		}

		log.SetOutput(f)
	}
}

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
			log.Printf("Unable to release pid-file: %s", err.Error())
		}
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

	repointLog(c)
	detachIfNeeded(c)

	fs, err := sdafs.NewSDAfs(c.sdafsconf)
	if err != nil {
		log.Fatalf("Error while creating sda fs: %v", err)
	}

	mount, err := fuse.Mount(c.mountPoint, fs.GetFileSystemServer(), mountConfig)
	if err != nil {
		log.Fatalf("Mount of sda at %s failed: %v", c.sdafsconf.RootURL, err)
	}
	log.Printf("SDA mount of %s ready at %s", c.sdafsconf.RootURL, c.mountPoint)

	afterMount(c, mount)
	log.Printf("SDA unmounted from %s, exiting", c.mountPoint)
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

		log.Printf("Received signal %v, exiting", s)
		// TODO: Retry on failure?
		err := fuse.Unmount(m)
		if err != nil {
			log.Fatalf("Unmounting failed %v", err)
		}
	}
}
