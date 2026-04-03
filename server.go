package main

import (
	"crypto/rand"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const TYPE_PIPE = "pipe"
const TYPE_FILE = "file"

var workBaseDir = "./tmp"
var streams = make(map[string]*Stream)
var allStreamsPlaylistEnabled = false

type Stream struct {
	Name         string
	Type         string
	Command      string
	Running      bool
	RunningMutex sync.Mutex
	Process      *os.Process
	WorkDir      string
	Viewers      atomic.Int32 // Only used for pipe
	StreamPipe   *os.File     // Only used for pipe
	MimeType     string       `toml:"mime-type"` // Only used for pipe
	Playlist     string       // Only used for non-pipe
	KillAt       int64
	Timeout      int64
}

func main() {
	configPath := "config.toml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	} else {
		println("No config path provided, using default:", configPath)
	}
	config, err := LoadConfig(configPath)
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}
	workBaseDir = config.WorkBaseDir
	streams = config.Streams
	allStreamsPlaylistEnabled = config.AllStreamsPlaylist

	go garbageCollector()

	// Stop all streams on interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		println("Shutting down, stopping all streams...")
		for _, stream := range streams {
			stopStream(stream)
		}
		os.Exit(0)
	}()

	// Finally, listen and serve
	http.HandleFunc("/", handler)
	println("Server is listening on", config.ListenOn)
	log.Fatal(http.ListenAndServe(config.ListenOn, nil))
}

func garbageCollector() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, stream := range streams {
			if !stream.Running {
				continue
			}

			// Pipe streams require regular viewer count checks
			if stream.Type == TYPE_PIPE {
				if stream.Viewers.Load() == 0 {
					if stream.KillAt == 0 {
						stream.KillAt = time.Now().Unix() + stream.Timeout
					}
				} else {
					stream.KillAt = 0
				}
			}

			if stream.KillAt != 0 && time.Now().Unix() > stream.KillAt {
				println("Stream timeout reached, stopping:", stream.Name)
				stopStream(stream)
			}
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		w.Write([]byte("OnlyOnDemand Streaming Server"))
		return
	}
	streamName := strings.Split(path[1:], "/")[0]
	if streamName == "all.m3u8" && allStreamsPlaylistEnabled {
		base := ""
		if r.URL.Query().Get("absolute") == "true" {
			base = "http://" + r.Host
		}
		w.Header().Add("Content-Type", "audio/x-mpegurl")
		var playlistBuilder strings.Builder
		playlistBuilder.WriteString("#EXTM3U\n")
		for name, stream := range streams {
			playlistBuilder.WriteString("#EXTINF:-1 tvg-id=\"" + name + "\"," + name + "\n")
			playlistBuilder.WriteString(base)
			if stream.Type == TYPE_PIPE {
				playlistBuilder.WriteString("/" + name + "\n")
			} else if stream.Type == TYPE_FILE {
				playlistBuilder.WriteString("/" + name + "/" + stream.Playlist + "\n")
			}
		}
		w.Write([]byte(playlistBuilder.String()))
		return
	}

	stream := streams[streamName]
	if stream == nil {
		// response status
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Stream not found"))
		return
	}

	filePath := path[len(streamName)+1:]
	if strings.HasPrefix(filePath, "/") {
		filePath = filePath[1:]
	}

	if stream.Type == TYPE_PIPE && filePath != "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Stream is a piped stream, do not specify a file path"))
		return
	}

	stream.RunningMutex.Lock()
	if !stream.Running {
		if stream.Type == TYPE_FILE && filePath != stream.Playlist {
			stream.RunningMutex.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Stream is not running, only playlist is available"))
			return
		}
		err := startStream(stream)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to start stream: " + err.Error()))
			stream.RunningMutex.Unlock()
			return
		}
	}
	stream.RunningMutex.Unlock()

	if stream.Type == TYPE_PIPE {
		w.Header().Add("Content-Type", stream.MimeType)
		// Pipe stdout to response
		stream.Viewers.Add(1)
		io.Copy(w, stream.StreamPipe)
		stream.Viewers.Add(-1)
		return
	}

	// Non-pipe, serve files from WorkDir
	// Prevent path traversal
	if strings.Contains(filePath, "..") {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid file path"))
		return
	}
	http.ServeFile(w, r, stream.WorkDir+"/"+filePath)
	// Reset Timeout
	stream.KillAt = time.Now().Unix() + stream.Timeout
}

func startStream(stream *Stream) error {
	println("Starting stream:", stream.Name)
	if stream.Running {
		panic("Stream is already running")
	}
	exists := true
	for exists {
		stream.WorkDir = workBaseDir + "/" + randomString(8)
		_, err := os.Stat(stream.WorkDir)
		if os.IsNotExist(err) {
			exists = false
			err = os.Mkdir(stream.WorkDir, 0755)
			if err != nil {
				return err
			}
		}
	}

	cmd := exec.Command("sh", "-c", stream.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir = stream.WorkDir

	if stream.Type == TYPE_PIPE {
		fifoPath := stream.WorkDir + "/output.pipe"
		err := syscall.Mkfifo(fifoPath, 0666)
		if err != nil {
			return err
		}
		stream.StreamPipe, err = os.OpenFile(fifoPath, os.O_RDWR, os.ModeNamedPipe)
		if err != nil {
			return err
		}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return err
	}

	go func() {
		_ = cmd.Wait()
		stopStream(stream)
	}()

	stream.Process = cmd.Process
	stream.Running = true
	if stream.Type == TYPE_FILE {
		// Wait for Playlist to be created
		stream.KillAt = time.Now().Unix() + stream.Timeout
		playlistPath := stream.WorkDir + "/" + stream.Playlist
		for {
			if _, err := os.Stat(playlistPath); err == nil {
				break
			}
			if time.Now().Unix() > stream.KillAt {
				println("Playlist not created in time, stopping stream:", stream.Name)
				stopStream0(stream)
				return errors.New("Stream failed to start: playlist not created in time")
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	return nil
}

func stopStream(stream *Stream) {
	stream.RunningMutex.Lock()
	defer stream.RunningMutex.Unlock()
	if !stream.Running {
		return
	}

	stopStream0(stream)
}
func stopStream0(stream *Stream) {
	println("Stopping stream:", stream.Name)
	if stream.Process != nil {
		_ = syscall.Kill(-stream.Process.Pid, syscall.SIGTERM)
		// todo maybe wait and then SIGKILL if not exited?
		stream.Process = nil
	}
	if stream.StreamPipe != nil {
		_ = stream.StreamPipe.Close()
		stream.StreamPipe = nil
	}
	if stream.WorkDir != "" {
		_ = os.RemoveAll(stream.WorkDir)
		stream.WorkDir = ""
	}
	stream.Running = false
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, n)
	_, _ = rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = letters[b%byte(len(letters))]
	}
	return string(bytes)
}
