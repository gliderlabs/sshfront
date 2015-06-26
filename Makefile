NAME=sshfront
OWNER=gliderlabs
ARCH=$(shell uname -m)
VERSION=0.2.0

build:
	mkdir -p build/Linux && GOOS=linux CGO_ENABLED=0 go build -a \
		-ldflags "-X main.Version $(VERSION)" \
		-installsuffix cgo \
		-o build/Linux/$(NAME)
	mkdir -p build/Darwin && GOOS=darwin CGO_ENABLED=0 go build -a \
		-ldflags "-X main.Version $(VERSION)" \
		-installsuffix cgo \
		-o build/Darwin/$(NAME)

deps:
	go get -u github.com/progrium/gh-release/...
	go get || true

example: build
	./build/Darwin/sshfront -d -p 2222 -k ~/.ssh/id_rsa example/helloworld

test:
	docker build -t $(NAME)-tests tests
	docker run --rm \
		-v $(PWD)/tests:/tests \
		-v $(PWD)/build/Linux/sshfront:/bin/sshfront \
		$(NAME)-tests \
		basht /tests/*.bash

release:
	rm -rf release && mkdir release
	tar -zcf release/$(NAME)_$(VERSION)_Linux_$(ARCH).tgz -C build/Linux $(NAME)
	tar -zcf release/$(NAME)_$(VERSION)_Darwin_$(ARCH).tgz -C build/Darwin $(NAME)
	gh-release create $(OWNER)/$(NAME) $(VERSION) $(shell git rev-parse --abbrev-ref HEAD) v$(VERSION)

circleci:
	rm ~/.gitconfig
	rm -rf /home/ubuntu/.go_workspace/src/github.com/$(OWNER)/$(NAME) && cd .. \
		&& mkdir -p /home/ubuntu/.go_workspace/src/github.com/$(OWNER) \
		&& mv $(NAME) /home/ubuntu/.go_workspace/src/github.com/$(OWNER)/$(NAME) \
		&& ln -s /home/ubuntu/.go_workspace/src/github.com/$(OWNER)/$(NAME) $(NAME)

.PHONY: build release
