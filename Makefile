.PHONY: all generate build desk studio web-install typecheck lint-web test vet lint scan pins clean

SURFACE := go run ./cmd/surface
APPS    := desk studio

# Generate, build the page and build the binary of every app into bin/.
all: $(APPS)

$(APPS):
	$(SURFACE) build -f apps/$@/surface.yaml

# Regenerate the wiring of every app after editing its surface.yaml.
generate:
	for a in $(APPS); do $(SURFACE) generate -f apps/$$a/surface.yaml || exit 1; done

web-install:
	npm ci

typecheck:
	npm run typecheck

lint-web:
	npm run lint

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run

# The go.mod pins must match the Substrate BOM; edit the BOM (in Substrate), never go.mod.
pins:
	go run github.com/hollis-labs/substrate/cmd/substrate pins check --app .

# Public-repo hygiene: secrets, internal hosts, private paths; tree and history.
scan:
	python3 scripts/scan-public --ref HEAD

# Remove build artifacts. Keeps each webui/dist/.gitkeep so go:embed still compiles.
clean:
	rm -rf bin
	for a in $(APPS); do find apps/$$a/webui/dist -mindepth 1 ! -name .gitkeep -delete; done
