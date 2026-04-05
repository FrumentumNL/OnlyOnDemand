package main

import (
	_ "embed"
	"os"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

//go:embed config.toml
var demoConfig []byte

type Config struct {
	WorkBaseDir        string             `toml:"work-base-dir"`
	ListenOn           string             `toml:"listen-on"`
	Streams            map[string]*Stream `toml:"streams"`
	Templates          []*Template        `toml:"template"`
	AllStreamsPlaylist bool               `toml:"all-streams-playlist"`
}

type Template struct {
	Stream
	Variants []map[string]string
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

		if !ValidateStream(stream, name, nil) {
			delete(config.Streams, name)
		}
	}

	if config.Templates != nil {
		for i, template := range config.Templates {
			if template == nil {
				println("Template", i, "is nil, skipping")
				continue
			}

			if template.Variants == nil || len(template.Variants) == 0 {
				println("Template", i, "has no variants, skipping")
				continue
			}

			for j, variant := range template.Variants {
				if variant == nil {
					println("Template", i, "variant", j, "is nil, skipping")
					continue
				}

				//goland:noinspection GoVetCopyLock
				stream := template.Stream
				name := ReplacePlaceholders(stream.Name, &variant)
				if !ValidateStream(&stream, name, &variant) {
					println("Template", i, "variant", j, "is invalid, skipping")
					continue
				}

				if config.Streams[name] != nil {
					println("Stream", name, "already exists, skipping template", i, "variant", j)
					continue
				}

				config.Streams[name] = &stream
			}
		}
	}

	return config, nil
}

func ValidateStream(stream *Stream, name string, placeholders *map[string]string) bool {
	stream.Command = ReplacePlaceholders(stream.Command, placeholders)
	if stream.Command == "" {
		println("Stream", name, "is missing command, skipping")
		return false
	}

	stream.Type = ReplacePlaceholders(stream.Type, placeholders)
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
			return false
		}
	default:
		println("Stream", name, "has invalid type, must be 'pipe' or 'file', skipping")
		return false
	}

	if stream.Timeout <= 0 {
		stream.Timeout = 30
		println("Stream", name, "has invalid timeout, defaulting to ", stream.Timeout, " seconds")
	}

	// Default some stuff
	// todo is there a way to just prevent unmarshalling these fields?
	stream.Name = ReplacePlaceholders(name, placeholders)
	stream.Running = false
	stream.RunningMutex = sync.Mutex{}

	return true
}

func ReplacePlaceholders(command string, placeholders *map[string]string) string {
	if placeholders == nil {
		return command
	}

	for key, value := range *placeholders {
		placeholder := "{" + key + "}"
		command = strings.ReplaceAll(command, placeholder, value)
	}
	return command
}
