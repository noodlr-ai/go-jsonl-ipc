# JSON Lines IPC for Go

A Go package for communicating with Python processes using JSON Lines (JSONL) over stdin/stdout. This package provides a simple and efficient way to spawn Python processes and communicate with them using structured JSON messages.

## Features

- **Simple IPC**: Communicate with Python processes using JSON Lines format
- **Process Management**: Automatic spawning and lifecycle management of Python processes
- **Async/Sync Communication**: Support for both synchronous RPC calls and asynchronous events
- **Type Safety**: Well-defined message structures with Go structs
- **Error Handling**: Comprehensive error handling and timeout support
- **Event System**: Register handlers for events sent from Python processes

## Installation

```bash
go mod init your-project
go get noodlr-ai/go-jsonl-ipc
```

# Tagging

```
git tag v0.0.x
git push origin v0.0.3
```