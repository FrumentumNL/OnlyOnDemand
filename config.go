package main

import (
	"os"
	"strings"
	"sync"

	_ "embed"

	"github.com/pelletier/go-toml/v2"
)

//go:embed config.toml
var demoConfig []byte

type Config struct {
	WorkBaseDir        string             `toml:"work-base-dir"`
	ListenOn           string             `toml:"listen-on"`
	Streams            map[string]*Stream `toml:"streams"`
	AllStreamsPlaylist bool               `toml:"all-streams-playlist"`
}

func LoadConfig(path string) (*Config, error) {
	config := &Config{}
	println("Reading config from:", path)
	var err error
	var data []byte
	if path != "demo" {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	} else {
		data = demoConfig
	}
	err = toml.Unmarshal(data, config)
	if err != nil {
		return nil, err
	}

	// Populate with defaults
	if config.WorkBaseDir == "" {
		config.WorkBaseDir = "./tmp"
		println("No work-base-dir provided, using default:", config.WorkBaseDir)
	}
	if config.ListenOn == "" {
		config.ListenOn = ":8080"
		println("No listen-on provided, using default:", config.ListenOn)
	}
	if config.Streams == nil {
		config.Streams = make(map[string]*Stream)
		println("No streams provided, using default: empty map")
	}
	for name, stream := range config.Streams {
		if stream == nil {
			println("Stream", name, "is nil, skipping")
			delete(config.Streams, name)
			continue
		}

		invalid := false
		if stream.Command == "" {
			println("Stream", name, "is missing command, skipping")
			invalid = true
		}

		if !invalid {
			switch strings.ToLower(stream.Type) {
			case "pipe":
				stream.Type = "pipe"
				if stream.MimeType == "" {
					stream.MimeType = "video/mp2t"
					println("Stream", name, "is a pipe but missing mime-type, defaulting to", stream.MimeType)
				}
			case "file":
				stream.Type = "file"
				if stream.Playlist == "" {
					println("Stream", name, "is not a pipe but missing playlist, skipping")
					invalid = true
				}
			default:
				println("Stream", name, "has invalid type, must be 'pipe' or 'file', skipping")
				invalid = true
			}
		}

		if !invalid && stream.Timeout <= 0 {
			stream.Timeout = 30
			println("Stream", name, "has invalid timeout, defaulting to ", stream.Timeout, " seconds")
		}

		if invalid {
			delete(config.Streams, name)
		} else {
			// Default some stuff
			// todo is there a way to just prevent unmarshalling these fields?
			stream.Name = name
			stream.Running = false
			stream.Viewers.Store(0)
			stream.RunningMutex = sync.Mutex{}
		}
	}

	return config, nil
}
