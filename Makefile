.PHONY: build

APP=nfqueue

build:
	go build -buildvcs=false -gcflags='all=-N -l' .

sync:
	rsync -auv --delete-after --exclude=.git ./ root@raspberrypi.local:nfqueue/

debug:
	dlv exec --headless --continue --accept-multiclient --listen=:56268 --api-version=2 $(APP)
#	dlv exec --accept-multiclient --listen=:56268 --api-version=2 $(APP)
