// YachtSense Link Emulator. All API writes and service transitions are owned by
// this static binary; Lua is only an authenticated RutOS routing adapter.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	p := discoverPaths()
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--version":
			fmt.Println(packageVersion)
			return
		case "--api":
			if len(args) != 2 {
				fmt.Fprintln(os.Stderr, "usage: --api status|diagnostics|save|start|stop|restart")
				os.Exit(2)
			}
			var out map[string]any
			var err error
			switch args[1] {
			case "status":
				out = currentStatus(p, false)
			case "diagnostics":
				out = currentStatus(p, true)
			default:
				var b []byte
				b, err = io.ReadAll(io.LimitReader(os.Stdin, 16385))
				if err == nil {
					out, err = requestAction(p, args[1], b)
				}
			}
			if err != nil {
				out = map[string]any{"ok": false, "message": err.Error()}
			}
			_ = json.NewEncoder(os.Stdout).Encode(out)
			return
		case "--worker":
			if len(args) != 3 {
				os.Exit(2)
			}
			if e := worker(p, args[1], args[2]); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
			return
		case "--is-enabled":
			c, e := readSettings(p)
			if e != nil || !c.Enabled {
				os.Exit(1)
			}
			return
		case "--daemon":
		default:
			fmt.Fprintln(os.Stderr, "unknown option")
			os.Exit(2)
		}
	}
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)
	log.SetPrefix("yachtsense-link-emulator: ")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if e := runDaemon(ctx, p); e != nil {
		log.Printf("ERROR %v", e)
		os.Exit(1)
	}
}
