package server

import (
	"net"
	"path/filepath"
	"sync"
	"testing"
)

// TestConnectionsArrivingAsTheServerClosesAreNotCountedAfterIt: a server
// being closed while connections keep arriving — a liveness probe, a client
// reconnecting — either serves one and waits for it, or drops it; it never
// counts one into the WaitGroup Close is already waiting on, which the race
// detector reports (TestListenRefusesALiveSocket found it in internal/client).
// If it regresses, `make check` fails now and then on a race, and a server
// stopping can miss a handler it should have waited for.
func TestConnectionsArrivingAsTheServerClosesAreNotCountedAfterIt(t *testing.T) {
	for range 20 {
		s, err := New(Config{})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "s.sock")
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		served := make(chan struct{})
		go func() { _ = s.Serve(ln); close(served) }()
		var dialers sync.WaitGroup
		stop := make(chan struct{})
		for range 4 {
			dialers.Add(1)
			go func() {
				defer dialers.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					if c, err := net.Dial("unix", path); err == nil {
						_ = c.Close()
					}
				}
			}()
		}
		_ = s.Close()
		close(stop)
		dialers.Wait()
		<-served
	}
}
