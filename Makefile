.PHONY: test build sync debug docker run-docker

default: build

APP=nfqueue
PACKAGE_TO_TEST=./nft
FUNC_TO_TEST=Test_addNFTablesRuleForSets

test:
	go test ./...

build:
	go build -buildvcs=false -gcflags='all=-N -l' -o $(APP) .

sync:
	rsync -auv --delete-after --exclude=.git ./ root@tubetimeout:nfqueue/

debug: build
	DEBUG_ENABLED=true dlv exec --headless --continue --accept-multiclient --listen=:56268 --api-version=2 $(APP)
#	dlv exec --accept-multiclient --listen=:56268 --api-version=2 $(APP)

debug-test:
	dlv test --headless --listen=:56268 --api-version=2 $(PACKAGE_TO_TEST) -- -test.run=$(FUNC_TO_TEST)

docker:
	docker build -t ubuntu-nftables-go .

run-docker:
	docker run -it --rm --cap-add=NET_ADMIN --cap-add=NET_RAW ubuntu-nftables-go
