// securespeak is the SecureSpeak desktop client. It runs the client core and
// shows the interface in a dedicated Edge/Chrome app window.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"securespeak/internal/client"
	"securespeak/internal/platform"
	"securespeak/internal/ui"
)

func main() {
	dataDirFlag := flag.String("data", "", "data directory (default: %APPDATA%\\SecureSpeak or ~/.config/SecureSpeak)")
	noWindow := flag.Bool("no-window", false, "do not open the app window; print the URL instead")
	flag.Parse()

	dataDir := *dataDirFlag
	if dataDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = "."
		}
		dataDir = filepath.Join(base, "SecureSpeak")
	}
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	store, err := client.NewStore(dataDir)
	if err != nil {
		fatal(err)
	}
	if f, err := os.OpenFile(filepath.Join(dataDir, "client.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); err == nil {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	}

	bridge := ui.NewBridge()
	core, err := client.New(store, bridge)
	if err != nil {
		fatal(err)
	}
	quit := make(chan struct{}, 1)
	bridge.SetHandler(func(cmd string, args json.RawMessage) (any, error) {
		res, err := core.HandleCommand(cmd, args)
		if err == client.ErrQuit {
			select {
			case quit <- struct{}{}:
			default:
			}
			return nil, nil
		}
		return res, err
	})
	core.SetVoiceSink(bridge.SendVoice)
	bridge.VoiceIn = core.HandleVoiceFrame
	bridge.Upload = core.OfferFile
	bridge.Avatar = core.Avatar
	bridge.AvatarCT = client.ImageMime
	url, err := bridge.Listen()
	if err != nil {
		fatal(err)
	}

	var windowClosed <-chan struct{}
	started := time.Now()
	if !*noWindow {
		windowClosed, err = platform.OpenWindow(url, filepath.Join(dataDir, "ui-profile"))
		if err != nil {
			log.Printf("window: %v", err)
		}
	}
	// Some launchers (e.g. a browser that hands the window to an already
	// running instance) exit immediately; then fall back to idle detection.
	if windowClosed != nil {
		select {
		case <-windowClosed:
			if time.Since(started) < 5*time.Second {
				log.Printf("browser launcher exited early; using idle detection")
				windowClosed = nil
			}
		case <-time.After(5 * time.Second):
		}
	}
	if windowClosed == nil {
		fmt.Println("SecureSpeak UI:", url)
	}
	log.Printf("SecureSpeak client started, identity %s", core.Fingerprint())

	// Without our own window (default-browser fallback) we exit once the
	// browser tab has been closed for a while.
	var idle <-chan time.Time
	if windowClosed == nil && !*noWindow {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		idle = t.C
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
wait:
	for {
		select {
		case <-windowClosed:
			break wait
		case <-quit:
			break wait
		case <-sig:
			break wait
		case <-idle:
			if bridge.Idle() > 30*time.Second {
				break wait
			}
		}
	}
	core.Close()
	time.Sleep(100 * time.Millisecond)
}

func fatal(err error) {
	log.Print(err)
	platform.Fatal("SecureSpeak", err.Error())
	os.Exit(1)
}
