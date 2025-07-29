# Amrita Engines

Core engine interfaces and types for the Amrita framework.

## Overview

This package provides the foundational engine interfaces and types used throughout the Amrita framework. It defines the core abstractions for processing engines, protocol buffer types, and shared utilities.

## Installation

```bash
go get github.com/bo-socayo/amrita-engines
```

## Usage

This package provides core interfaces and types that are implemented by other Amrita framework components.

## Package Structure

- `pkg/` - Core Go packages with engine interfaces and types
- `proto/` - Protocol buffer definitions
- `gen/` - Generated protobuf Go code

## Dependencies

- Protocol Buffers support
- Buf for protocol buffer generation
- Protovalidate for validation

## Development

To regenerate protocol buffer code:

```bash
buf generate
```

## License

Part of the Amrita framework. 