ifeq ($(REGISTRY),)
        REGISTRY = registry.cn-hangzhou.aliyuncs.com/wise2c-test/
endif
ifeq ($(VERSION),)
        VERSION = latest
endif
IMAGE = $(REGISTRY)nfs-client-provisioner:$(VERSION)
MUTABLE_IMAGE = $(REGISTRY)nfs-client-provisioner:latest

IMAGE?=nfs-client-provisioner

all: dep nfs-client-provisioner image

dep:
	-dep init
	dep ensure

nfs-client-provisioner:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o nfs-client-provisioner .

image: nfs-client-provisioner
	docker build --no-cache=true -t $(IMAGE) -f Dockerfile.scratch .
	docker tag $(MUTABLE_IMAGE) $(IMAGE)

clean:
	rm -rf vendor
	rm -rf Gopkg.lock
	rm -rf nfs-client-provisioner
.PHONY: nfs-client-provisioner