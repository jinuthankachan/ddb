.PHONY: build clean run-node run-client

build: build-node build-client

build-node:
	go build -o bin/node cmd/node/main.go

build-client:
	go build -o bin/client cmd/client/main.go

clean:
	rm -rf bin/

run-node:
	./bin/node \
		--id $(ID) \
		--addr $(ADDR) \
		--peers "$(PEERS)" \
		--f $(F)

run-client:
	./bin/client \
		--node $(NODE) \
		--op $(OP) \
		--key $(KEY) \
		--value $(VALUE)

# Example usage:
# Run node:
# make run-node ID=node0 ADDR=localhost:8080 PEERS="node1=localhost:8081,node2=localhost:8082,node3=localhost:8083" F=1
#
# Run client:
# make run-client NODE=localhost:8080 OP=set KEY=mykey VALUE="Hello, world!"
# make run-client NODE=localhost:8080 OP=get KEY=mykey
#
