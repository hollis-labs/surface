.PHONY: all generate build desk studio web-install typecheck lint-web ui-build ui-verify ui-shots packages pack-check test vet lint scan pins clean

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

# Regenerate the @surface registry from registry/ui (registry/README.md).
ui-build:
	node scripts/ui.mjs build

# The components copied into the page must be exactly the registry's.
ui-verify:
	node scripts/ui.mjs verify

# Photograph the component gallery (Components window) in dark and light, outside the repo.
# OUT=<dir> to choose where; the default is $$TMPDIR/surface-ui-shots.
ui-shots: studio
	node scripts/ui-shots.mjs --app studio $(if $(OUT),--out $(OUT))

# Build the npm packages (packages/*: chat, cards, genui, page) into each dist/.
packages:
	npm run build:packages

# Pack every published package, install the tarballs in an empty directory and check they work there.
pack-check:
	npm run pack-check

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
