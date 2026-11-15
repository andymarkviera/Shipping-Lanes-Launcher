package main

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const repository = "https://api.github.com/repos/andymarkviera/Shipping-Lanes-Releases"
const maxArchive = 1 << 30

//go:embed launcher.html
var page string

//go:embed logo.webp
var logo []byte

type asset struct {
	URL    string `json:"browser_download_url"`
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type release struct {
	Tag       string    `json:"tag_name"`
	Published time.Time `json:"published_at"`
	Draft     bool      `json:"draft"`
	Assets    []asset   `json:"assets"`
}

type installed struct {
	Asset int64  `json:"asset"`
	Tag   string `json:"tag"`
	Entry string `json:"entry"`
}

type status struct {
	Username string `json:"username"`
	Message  string `json:"message"`
	Busy     bool   `json:"busy"`

	Available  string `json:"available"`
	Installed  string `json:"installed"`
	CanInstall bool   `json:"canInstall"`
	Progress   int    `json:"progress"`
}

type launcher struct {
	accountState    string
	accountName     string
	accountSubject  string
	accountDeadline time.Time
	mu              sync.Mutex
	root            string

	release release
	asset   asset
	active  installed
	state   status
	client  *http.Client
}

func main() {
	root, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	root = filepath.Join(root, "Shipping Lanes Launcher", "builds")
	app := &launcher{
		root:   root,
		active: findInstalled(root),
		state:  status{Message: "Checking GitHub for the latest game build.", Busy: true},
		client: &http.Client{Timeout: 15 * time.Minute, CheckRedirect: githubRedirect},
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	host := listener.Addr().String()
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		panic(err)
	}
	key := "/" + hex.EncodeToString(secret[:]) + "/"
	origin := "http://" + host
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8192,
	}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host || (!strings.HasPrefix(r.URL.Path, key) && r.URL.Path != "/account-callback") || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin) {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/account-callback" {
			app.callback(w, r, origin, key)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; base-uri 'none'")
		path := strings.TrimPrefix(r.URL.Path, key)
		switch {
		case r.Method == "GET" && path == "":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, strings.Replace(page, "{{LAUNCHER_LOGO}}", base64.StdEncoding.EncodeToString(logo), 1))
		case r.Method == "GET" && path == "status":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(app.snapshot())
		case r.Method == "POST" && r.Header.Get("Origin") == origin:
			app.handle(w, r, path, server)
		default:
			http.NotFound(w, r)
		}
	})
	go app.check()
	if err := openURL(origin + key); err != nil {
		panic(err)
	}
	_ = server.Serve(listener)
}

func openURL(address string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", address).Run()
	}
	return exec.Command("/usr/bin/open", address).Run()
}

func (l *launcher) snapshot() status {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state
	s.Username = l.accountName
	s.Installed = l.active.Tag
	s.Available = l.release.Tag
	s.CanInstall = l.asset.ID != 0 && l.asset.ID != l.active.Asset && !s.Busy
	return s
}

func (l *launcher) handle(w http.ResponseWriter, r *http.Request, path string, server *http.Server) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if path == "signin" {
		if err := l.beginSignIn(r.Host); err != nil {
			http.Error(w, "Could not start sign-in.", 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if path == "signout" {
		l.accountState, l.accountName, l.accountSubject = "", "", ""
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if path == "play" && l.accountSubject == "" {
		http.Error(w, "Sign in to play.", http.StatusForbidden)
		return
	}
	if path == "play" && l.active.Asset != 0 {
		entry := filepath.Join(l.root, strconv.FormatInt(l.active.Asset, 10), l.active.Entry)
		var command *exec.Cmd
		if runtime.GOOS == "darwin" {
			command = exec.Command("/usr/bin/open", entry)
		} else {
			command = exec.Command(entry)
			command.Dir = filepath.Dir(entry)
		}
		if err := command.Start(); err != nil {
			l.state.Message = "The game could not open. Check that the installed game files are still present."
		} else {
			l.state.Message = "Opening Shipping Lanes…"
			go func() {
				if err := command.Wait(); err != nil && runtime.GOOS == "darwin" {
					l.mu.Lock()
					l.state.Message = "macOS could not open this build. Check the app in Finder or review macOS Privacy & Security."
					l.mu.Unlock()
				}
			}()
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if path == "quit" {
		w.WriteHeader(http.StatusNoContent)
		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = server.Close()
		}()
		return
	}
	if l.state.Busy {
		http.Error(w, "Another operation is running.", http.StatusConflict)
		return
	}
	switch path {
	case "check":
		l.state = status{Busy: true, Message: "Checking GitHub…"}
		go l.check()
	case "install":
		if l.asset.ID == 0 || l.asset.ID == l.active.Asset {
			http.Error(w, "No update available.", http.StatusConflict)
			return
		}
		l.state = status{Busy: true, Message: "Downloading the game…"}
		go l.install(l.release.Tag, l.asset)
	default:
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func githubRedirect(req *http.Request, via []*http.Request) error {
	host := req.URL.Hostname()
	if len(via) >= 5 || req.URL.Scheme != "https" || (host != "api.github.com" && host != "github.com" && !strings.HasSuffix(host, ".githubusercontent.com")) {
		return errors.New("unexpected GitHub download redirect")
	}
	return nil
}

func (l *launcher) request(address, accept string) (*http.Response, error) {
	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "Shipping-Lanes-Launcher/1.1")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := l.client.Do(req)
	if err != nil {
		return nil, errors.New("Could not reach GitHub. Check your connection and try again.")
	}
	switch response.StatusCode {
	case http.StatusOK:
		return response, nil
	case http.StatusNotFound:
		response.Body.Close()
		return nil, errors.New("The requested release is unavailable on GitHub.")
	case http.StatusForbidden, http.StatusTooManyRequests:
		response.Body.Close()
		return nil, errors.New("GitHub is temporarily limiting requests. Please try again later.")
	default:
		response.Body.Close()
		return nil, fmt.Errorf("GitHub returned HTTP %d. Try again later.", response.StatusCode)
	}
}

func (l *launcher) check() {
	response, err := l.request(repository+"/releases/latest", "application/vnd.github+json")
	var latest release
	if err == nil {
		defer response.Body.Close()
		if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&latest) != nil {
			err = errors.New("Could not read GitHub release information.")
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state = status{}
	l.asset, l.release = asset{}, release{}
	if err != nil {
		l.state.Message = err.Error()
		return
	}
	name := "Shipping-Lanes-Windows-x64.zip"
	if runtime.GOOS == "darwin" {
		name = "Shipping-Lanes-macOS.zip"
	}
	prefix := "https://github.com/andymarkviera/Shipping-Lanes-Releases/releases/download/"
	for _, file := range latest.Assets {
		link, err := url.Parse(file.URL)
		if latest.Draft || !strings.EqualFold(file.Name, name) || file.ID <= 0 || file.Size <= 0 ||
			file.Size > maxArchive || err != nil || link.Host != "github.com" || link.User != nil ||
			!strings.HasPrefix(file.URL, prefix) || filepath.Base(link.Path) != file.Name {
			continue
		}
		l.asset, l.release = file, latest
		if l.active.Asset == file.ID {
			l.state.Message = "You have the latest build."
		} else {
			l.state.Message = "A game build is ready to download."
		}
		return
	}
	l.state.Message = "The latest release has no compatible package for your platform."
}

type downloadProgress struct {
	app            *launcher
	current, total int64
}

func (p *downloadProgress) Write(data []byte) (int, error) {
	p.current += int64(len(data))
	p.app.mu.Lock()
	p.app.state.Progress = int(p.current * 100 / p.total)
	p.app.mu.Unlock()
	return len(data), nil
}

func (l *launcher) install(tag string, file asset) {
	result, err := l.installBuild(tag, file)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state = status{}
	if err != nil {
		l.state.Message = err.Error()

		return
	}
	l.active = result
	l.state.Message = "Download complete. Ready to play."
}

func (l *launcher) installBuild(tag string, file asset) (installed, error) {
	var result installed
	if err := os.MkdirAll(l.root, 0700); err != nil {
		return result, err
	}
	final := filepath.Join(l.root, strconv.FormatInt(file.ID, 10))
	if existing, ok := readReceipt(final); ok {
		return existing, nil
	}
	stage, err := os.MkdirTemp(l.root, ".install-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "game.zip")
	output, err := os.Create(archive)
	if err != nil {
		return result, err
	}
	defer output.Close()

	response, err := l.request(file.URL, "application/octet-stream")
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	digest := sha256.New()
	progress := &downloadProgress{app: l, total: file.Size}
	count, err := io.Copy(io.MultiWriter(output, digest, progress), io.LimitReader(response.Body, file.Size+1))
	if err != nil || count != file.Size {
		return result, errors.New("Download incomplete. Try downloading again.")
	}
	if err := output.Close(); err != nil {
		return result, err
	}
	if file.Digest != "" && !strings.EqualFold(file.Digest, "sha256:"+hex.EncodeToString(digest.Sum(nil))) {
		return result, errors.New("The download failed its checksum. Try downloading again.")
	}
	l.mu.Lock()
	l.state.Message = "Installing the game…"
	l.mu.Unlock()
	payload := filepath.Join(stage, "payload")
	if err := unpack(archive, payload); err != nil {
		return result, fmt.Errorf("Could not install the game: %w", err)
	}
	entry, err := findGame(payload)
	if err != nil {
		return result, err
	}
	result = installed{Asset: file.ID, Tag: tag, Entry: entry}
	receipt, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(payload, ".shipping-lanes-build.json"), receipt, 0600); err != nil {
		return result, err
	}
	if err := os.Rename(payload, final); err != nil {
		if existing, ok := readReceipt(final); ok {
			return existing, nil
		}
		return result, err
	}
	return result, nil
}

func unpack(archive, target string) error {
	files, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer files.Close()
	if len(files.File) > 12000 {
		return errors.New("too many archive entries")
	}
	var total uint64
	for _, item := range files.File {
		name := strings.TrimSuffix(item.Name, "/")
		if !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") {
			return errors.New("unsafe archive path")
		}
		path := filepath.Join(target, filepath.FromSlash(name))
		if item.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
			continue
		}
		if !item.Mode().IsRegular() || item.UncompressedSize64 > 3*maxArchive-total {
			return errors.New("unsupported or oversized archive entry")
		}
		total += item.UncompressedSize64
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := extract(item, path, name); err != nil {
			return err
		}
	}
	return nil
}

func extract(item *zip.File, path, name string) error {
	stream, err := item.Open()
	if err != nil {
		return err
	}
	defer stream.Close()
	mode := os.FileMode(0644)
	if item.Mode()&0111 != 0 || strings.Contains(name, ".app/Contents/MacOS/") {
		mode = 0755
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copied := io.Copy(file, stream)
	closed := file.Close()
	return errors.Join(copied, closed)
}

func findGame(root string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if runtime.GOOS == "darwin" && entry.IsDir() && strings.EqualFold(entry.Name(), "Shipping Lanes.app") {
			if _, err := os.Stat(filepath.Join(path, "Contents", "Info.plist")); err == nil {
				matches = append(matches, path)
			}
			return filepath.SkipDir
		}
		if runtime.GOOS == "windows" && !entry.IsDir() && strings.EqualFold(entry.Name(), "ShippingLanes.exe") {
			if _, err := os.Stat(strings.TrimSuffix(path, filepath.Ext(path)) + ".pck"); err == nil {
				matches = append(matches, path)
			}
		}
		return nil
	})
	if err != nil || len(matches) != 1 {
		return "", errors.New("The package does not contain a complete Shipping Lanes game for this platform.")
	}
	return filepath.Rel(root, matches[0])
}

func readReceipt(dir string) (installed, bool) {
	var result installed
	raw, err := os.ReadFile(filepath.Join(dir, ".shipping-lanes-build.json"))
	if err != nil || json.Unmarshal(raw, &result) != nil || result.Asset <= 0 || !filepath.IsLocal(result.Entry) {
		return installed{}, false
	}
	if filepath.Base(dir) != strconv.FormatInt(result.Asset, 10) {
		return installed{}, false
	}
	_, err = os.Stat(filepath.Join(dir, result.Entry))
	return result, err == nil
}

func findInstalled(root string) installed {
	var latest installed
	dirs, _ := os.ReadDir(root)
	for _, dir := range dirs {
		if result, ok := readReceipt(filepath.Join(root, dir.Name())); ok && result.Asset > latest.Asset {
			latest = result
		}
	}
	return latest
}
