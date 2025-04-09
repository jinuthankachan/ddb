# DDB

* DDB is a simple in-memory distributed database that provides a key-value store with support for replication and fault tolerance.
* It is not designed to be a production-ready database, but rather a learning tool for understanding distributed systems.
* Tools and Protocols
  * Language: Golang
  * Consensus: PBFT
  * Storage: In-memory
  * Networking: HTTP
  * Purpose: Learning exercise
* Reference: This is implemented referring [this](./docs/reference.md)

## Checkpoints

- [x] Basic Functionality
  We've created a basic distributed key-value store using PBFT consensus. This implementation covers:

  1. A thread-safe in-memory key-value store
  2. PBFT message structures and state management
  3. HTTP-based network communication
  4. Core PBFT consensus logic (normal case operation)
  5. Client interaction API

  Some important aspects are simplified or omitted for clarity:

  1. View changes: The implementation focuses on the normal case
  2. Checkpointing: Basic state cleaning is included but not full checkpoint handling
  3. Message authentication: We don't implement full cryptographic signatures
  4. Error handling: Some error cases are simplified
  5. Client session handling: We don't track client request timestamps for duplicate filtering

- [ ] Next Steps

  To evolve this implementation, we could:

  1. Add proper view change handling
  2. Implement full checkpoint synchronization
  3. Add proper cryptographic signatures
  4. Improve client handling with request sequence tracking
  5. Add persistence to the key-value store
  6. Enhance error handling and recovery
