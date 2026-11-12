// securespeak is the SecureSpeak desktop client. It runs the client core and
// shows the interface in a dedicated Edge/Chrome app window.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"securespeak/internal/client"
	"securespeak/internal/platform"
	"securespeak/internal/ui"
	"securespeak/internal/update"
)

// version is set at build time: -ldflags "-X main.version=3.0.1"
var version = "dev"

type instanceInfo struct {
	URL string `json:"url"`
	PID int    `json:"pid"`
}

// runningInstance returns the UI address of an already running SecureSpeak
// using the same data directory, or "".
func runningInstance(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var inst instanceInfo
	if json.Unmarshal(b, &inst) != nil {
		return ""
	}
	u, err := url.Parse(inst.URL)
	if err != nil || u.Hostname() != "127.0.0.1" {
		return ""
	}
	cl := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := cl.Get("http://" + u.Host + "/ping?token=" + url.QueryEscape(u.Query().Get("token")))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	if resp.StatusCode != 200 || string(body) != "securespeak" {
		return ""
	}
	return inst.URL
}

func main() {
	dataDirFlag := flag.String("data", "", "data directory (default: %APPDATA%\\SecureSpeak or ~/.config/SecureSpeak)")
	noWindow := flag.Bool("no-window", false, "do not open the app window; print the URL instead")
	showVersion := flag.Bool("version", false, "print the version and exit")
	afterUpdate := flag.Bool("after-update", false, "started by the updater: wait for the previous version to exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("SecureSpeak", version)
		return
	}
	client.AppVersion = version

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
	profileDir := filepath.Join(dataDir, "ui-profile")

	// Single instance: if SecureSpeak is already running with this data
	// folder, just bring up its window (e.g. clicking the Start menu entry again).
	instPath := filepath.Join(dataDir, "instance.json")
	if *afterUpdate {
		// the old version is still shutting down
		for i := 0; i < 100 && runningInstance(instPath) != ""; i++ {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if u := runningInstance(instPath); u != "" {
		if *noWindow {
			fmt.Println("SecureSpeak is already running:", u)
		} else if _, err := platform.OpenWindow(u, profileDir); err != nil {
			log.Printf("window: %v", err)
		}
		return
	}
	if f, err := os.OpenFile(filepath.Join(dataDir, "client.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); err == nil {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	}

	bridge := ui.NewBridge()
	core, err := client.New(store, bridge)
	if err != nil {
		fatal(err)
	}
	go platform.RefreshUserIcon(ui.Icon())
	upd := update.New(version, filepath.Join(dataDir, "update"))
	upd.Cleanup()
	updates := update.NewManager(upd, func(s update.State) { bridge.Emit("update", s) }, core.AutoUpdate)
	if *afterUpdate {
		updates.MarkUpdated()
	}
	quit := make(chan struct{}, 1)
	bridge.SetHandler(func(cmd string, args json.RawMessage) (any, error) {
		switch cmd {
		case "update_state":
			return updates.State(), nil
		case "update_check":
			updates.Check()
			return nil, nil
		case "update_install":
			return nil, updates.Install()
		case "update_auto":
			var a struct{ On bool }
			json.Unmarshal(args, &a)
			core.SetAutoUpdate(a.On)
			updates.Publish()
			return nil, nil
		}
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
	bridge.VideoIn = core.HandleVideoFrame
	bridge.Media = core.MediaFile
	bridge.Upload = core.OfferFile
	bridge.Avatar = core.Avatar
	bridge.AvatarCT = client.ImageMime
	uiURL, err := bridge.Listen()
	if err != nil {
		fatal(err)
	}
	if b, err := json.Marshal(instanceInfo{URL: uiURL, PID: os.Getpid()}); err == nil {
		os.WriteFile(instPath, b, 0600)
		defer func() {
			if cur, err := os.ReadFile(instPath); err == nil && string(cur) == string(b) {
				os.Remove(instPath)
			}
		}()
	}

	var windowClosed <-chan struct{}
	started := time.Now()
	if !*noWindow {
		windowClosed, err = platform.OpenWindow(uiURL, profileDir)
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
		fmt.Println("SecureSpeak UI:", uiURL)
	}
	log.Printf("SecureSpeak %s started (%s install), identity %s", version, upd.Kind, core.Fingerprint())
	updates.Publish()
	runCtx, stopRun := context.WithCancel(context.Background())
	defer stopRun()
	go updates.Run(runCtx, 5*time.Second)

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
	var pending *update.Pending
	windowGone := false
wait:
	for {
		select {
		case <-windowClosed:
			windowGone = true
			break wait
		case pending = <-updates.Restart:
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
	stopRun()

	// Close the app window: the page closes itself on "closing"; if it does
	// not, end the browser process we started.
	bridge.Emit("closing", map[string]bool{"restart": pending != nil})
	core.Close()
	if windowClosed != nil && !windowGone {
		select {
		case <-windowClosed:
		case <-time.After(2500 * time.Millisecond):
			platform.CloseWindow()
			select {
			case <-windowClosed:
			case <-time.After(time.Second):
			}
		}
	} else {
		time.Sleep(300 * time.Millisecond)
	}
	if pending != nil {
		var args []string
		if *dataDirFlag != "" {
			args = append(args, "-data", dataDir)
		}
		if *noWindow {
			args = append(args, "-no-window")
		}
		log.Printf("restarting into SecureSpeak %s", pending.Version)
		if err := pending.Finish(args); err != nil {
			log.Printf("update: %v", err)
			platform.Fatal("SecureSpeak", "The update was downloaded but SecureSpeak could not be restarted: "+err.Error()+"\n\nStart SecureSpeak again manually.")
		}
	}
	time.Sleep(100 * time.Millisecond)
}

func fatal(err error) {
	log.Print(err)
	platform.Fatal("SecureSpeak", err.Error())
	os.Exit(1)
}
