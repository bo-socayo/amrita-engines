# Amrita Engines

Core engine interfaces, types, and entity workflows for the Amrita framework.

## Overview

This package provides the foundational engine system for the Amrita framework, including:

- **Engine Interfaces & Types**: Core abstractions for processing engines, protocol buffer types, and shared utilities
- **Entity Workflows**: High-level workflow patterns and execution models for entity-based processing

## Installation

```bash
go get github.com/bo-socayo/amrita-engines
```

## Usage

This package provides core interfaces and types that are implemented by other Amrita framework components.

## Package Structure

- `pkg/engines/` - Core engine interfaces and types
- `pkg/entityworkflows/` - Entity workflow patterns and execution models
- `proto/engines/` - Engine protocol buffer definitions
- `proto/entity/` - Entity workflow protocol buffer definitions  
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