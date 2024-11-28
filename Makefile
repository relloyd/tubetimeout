.PHONY: build

APP=nfqueue

build:
	go build -buildvcs=false -gcflags='all=-N -l' -o $(APP) .

sync:
	rsync -auv --delete-after --exclude=.git ./ root@raspberrypi:nfqueue/

debug: build
	DEBUG_ENABLED=true dlv exec --headless --continue --accept-multiclient --listen=:56268 --api-version=2 $(APP)
#	dlv exec --accept-multiclient --listen=:56268 --api-version=2 $(APP)

docker:
	docker build -t ubuntu-nftables-go .

run-docker:
	docker run -it --rm --cap-add=NET_ADMIN --cap-add=NET_RAW ubuntu-nftables-go
