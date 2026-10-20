package web

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Status is what a running tend web says of itself, written beside the
// session's state once it listens: which process it is and where a phone
// reaches it. The session's server reads it to say whether the phone view
// is on and at which addresses, and to stop it (the menu's "phone view").
// The addresses carry the token, so the file is the user's alone, as the
// token's own file is.
type Status struct {
	Pid    int      `json:"pid"`
	Listen string   `json:"listen"`
	URLs   []string `json:"urls"`
}

// StatusPath is where a session's tend web writes its status.
func StatusPath(stateDir, session string) string {
	return filepath.Join(stateDir, "web-"+session+".json")
}

// WriteStatus writes the status whole, readable by the user only.
func WriteStatus(path string, st Status) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".web-status-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ReadStatus reads what a tend web wrote. A file there says nothing of
// whether that process is still alive; the reader asks that.
func ReadStatus(path string) (Status, error) {
	var st Status
	data, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

// URLs are the addresses a phone can reach the page at, the token on each:
// every address of this machine but the loopback, when listening on all of
// them.
func URLs(addr *net.TCPAddr, token string) []string {
	port := strconv.Itoa(addr.Port)
	var hosts []string
	if addr.IP.IsUnspecified() {
		ifaces, _ := net.Interfaces()
		for _, ifc := range ifaces {
			if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || virtualInterface(ifc.Name) {
				continue
			}
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
					hosts = append(hosts, ipn.IP.String())
				}
			}
		}
	} else {
		hosts = []string{addr.IP.String()}
	}
	var out []string
	for _, h := range hosts {
		out = append(out, "http://"+net.JoinHostPort(h, port)+"/?t="+token)
	}
	return out
}

// virtualInterface is a network only this machine sits on — Docker's
// bridges, a virtual machine's — whose address a phone never reaches.
func virtualInterface(name string) bool {
	for _, p := range []string{"docker", "br-", "virbr", "veth", "vmnet", "vboxnet", "cni", "flannel", "podman", "incusbr", "lxdbr"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
