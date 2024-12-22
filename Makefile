.PHONY: test build sync debug docker run-docker install

default: build

APP=tubetimeout
APP_SHORT=tt
INSTALL_DEST := /usr/local/bin
INSTALL_TIMESTAMP := $(shell date +"%Y%m%d%H%M%S")

PACKAGE_TO_TEST=./nft
FUNC_TO_TEST=Test_addNFTablesRuleForSingleDestAddr

test:
	go test ./...

build:
	go build -buildvcs=false -gcflags='all=-N -l' -o $(APP_SHORT) .

build-release: test
	go build -ldflags "-s -w" -gcflags "all=-trimpath=$(pwd)" -o $(APP_SHORT) .

sync:
	rsync -auv --delete-after --exclude=.git ./ root@tubetimeout:tubetimeout/

debug: build
	DEBUG_ENABLED=true dlv exec --headless --continue --accept-multiclient --listen=:56268 --api-version=2 $(APP_SHORT)
#	dlv exec --accept-multiclient --listen=:56268 --api-version=2 $(APP_SHORT)

debug-test:
	dlv test --headless --listen=:56268 --api-version=2 $(PACKAGE_TO_TEST) -- -test.run=$(FUNC_TO_TEST)

docker:
	docker build -t ubuntu-nftables-go .

run-docker:
	docker run -it --rm --cap-add=NET_ADMIN --cap-add=NET_RAW ubuntu-nftables-go

install: build-release
	@echo "Installing $(APP) with timestamp $(INSTALL_TIMESTAMP)..."
	install -m 0755 $(APP_SHORT) $(INSTALL_DEST)/$(APP)-$(INSTALL_TIMESTAMP)
	ln -sf $(INSTALL_DEST)/$(APP)-$(INSTALL_TIMESTAMP) $(INSTALL_DEST)/tt
	@echo "Installation complete!"
