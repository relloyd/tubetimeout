.PHONY: test build build-release install sync debug docker run-docker install-daemon logs

default: build

APP=tubetimeout
APP_SHORT=tt
INSTALL_DEST := /usr/local/bin
INSTALL_TIMESTAMP := $(shell date +"%Y%m%dT%H%M%S")

PACKAGE_TO_TEST=./dhcp
FUNC_TO_TEST=TestGetConfigLoads

LD_FLAGS=-ldflags "-X relloyd/tubetimeout/config.BuildTime=$$(date -u +%Y-%m-%dT%H:%M:%SZ) -X relloyd/tubetimeout/config.BuildVersion=$$(git describe --tags --always --dirty)"

test:
	go test ./...

build:
	go build -buildvcs=false -gcflags 'all=-N -l' $(LD_FLAGS) -o $(APP_SHORT) .

build-release: test
	go build -ldflags "-s -w" $(LD_FLAGS) -gcflags "all=-trimpath=$(pwd)" -o $(APP_SHORT) .

debug: build
	DEBUG_ENABLED=true LOG_LEVEL=debug dlv exec --headless --continue --accept-multiclient --listen=:56268 --api-version=2 $(APP_SHORT)
	#	dlv exec --accept-multiclient --listen=:56268 --api-version=2 $(APP_SHORT)

debug-test:
	DEBUG_ENABLED=true LOG_LEVEL=debug dlv test --headless --listen=:56268 --api-version=2 $(PACKAGE_TO_TEST) -- -test.run=$(FUNC_TO_TEST)

run: build
	LOG_LEVEL=info DELAY_START=false $(APP_SHORT)

run-debug: build
	LOG_LEVEL=debug DELAY_START=false DHCP_SERVER_DISABLED=true $(APP_SHORT)

install: build-release
	@echo "Installing $(APP) with timestamp $(INSTALL_TIMESTAMP)..."
	install -m 0755 $(APP_SHORT) $(INSTALL_DEST)/$(APP)-$(INSTALL_TIMESTAMP)
	ln -sf $(INSTALL_DEST)/$(APP)-$(INSTALL_TIMESTAMP) $(INSTALL_DEST)/tt
	@echo "Installation complete!"

install-and-restart: stop install start
	@echo Done.

sync:
	rsync -auv --delete-after ./ root@tubetimeout.local:tubetimeout/

sync2:
	rsync -auv --delete-after ./ root@orangepizero3.local:tubetimeout/

docker:
	# Build docker image for local testing of nftables which is not available on MacOS
	docker build -t ubuntu-nftables-go .

run-docker:
	# Run docker container with nftables capabilities
	docker run -it --rm --cap-add=NET_ADMIN --cap-add=NET_RAW ubuntu-nftables-go

daemon:
	cp -p services/tubetimeout.service /etc/systemd/system/
	cp -p services/tubetimeout-netfilter-settings.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable tubetimeout
	sysctmctl enable tubetimeout-netfilter-settings.service

start:
	systemctl start tubetimeout

stop:
	systemctl stop tubetimeout

status:
	systemctl status tubetimeout

disable:
	systemctl disable tubetimeout

enable:
	systemctl enable tubetimeout

logs:
	journalctl -u tubetimeout.service -f
