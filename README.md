# OnlyOnDemand

Live-streaming server that only produces stream outputs when someone is actually watching.
This allows you to save CPU resources and bandwidth when no one is watching, while still providing a seamless streaming
experience when someone tunes in.

> **NOTE:** OnlyOnDemand is beta software! It may contain bugs. If you find any, please report them in the
> [issue tracker](https://github.com/FrumentumNL/OnlyOnDemand/issues).
> This is my first big Golang project, so the codebase is probably not as clean as you might expect from other Go-based
> projects. I'm working on cleaning it up, feedback is welcome :)

## Features

- Supports continuos MPEG-TS streams and HLS streaming.
- Automatically starts and stops the stream based on viewer activity.
- Supports any stream source that can be started and stopped via command line (e.g. FFMPEG)
- Supports custom timeouts for starting and stopping the stream.

## Use cases

OnlyOnDemand is ideal for scenarios where you might not always have viewers and keeping the stream running would waste
resources.

It is probably NOT a fit for you if you have an extremely large number of viewers (use a proper HLS CDN), if you want
your broadcast to have very quick start latency, or if you always have at least one viewer.

## Example usage

See the included [default configuration file](./config.toml) for two examples: one for a continuous MPEG-TS stream and
one for an HLS stream.

```bash
# Start the server with a custom config file
./onlyondemand ./path/to/config.toml

# Start the server with the demo config file
./onlyondemand demo
```

## License & credit

OnlyOnDemand is licensed under the [MIT License](./LICENSE).
It is the (spiritual) successor to [HLSify](https://github.com/NoahvdAa/HLSify).
