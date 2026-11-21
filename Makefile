# Arch Linux uses WebKit2GTK 4.1; Wails needs this build tag.
TAGS := webkit2_41

.PHONY: dev build test test-race fmt vet clean release

dev:
	wails dev -tags $(TAGS)

build:
	wails build -tags $(TAGS)

test:
	go test -tags $(TAGS) ./...

test-race:
	go test -race -tags $(TAGS) ./...

fmt:
	gofmt -w app.go main.go internal/

vet:
	go vet -tags $(TAGS) ./...

clean:
	rm -rf build/bin frontend/dist

# Produces dist/ archives for the host OS (Linux also builds Windows).
release:
	bash scripts/build-release.sh
