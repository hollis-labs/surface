.PHONY: all generate build demo web-install typecheck lint-web ui-build ui-verify ui-shots design-preview design-gallery packages pack-check test vet lint scan pins clean

SURFACE := go run ./cmd/surface
APPS    := demo

# Generate, build the page and build the binary of every app into bin/.
all: $(APPS)

$(APPS):
	$(SURFACE) build -f examples/$@/surface.yaml

# Regenerate the wiring of every app after editing its surface.yaml.
generate:
	for a in $(APPS); do $(SURFACE) generate -f examples/$$a/surface.yaml || exit 1; done

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

# The component gallery on its own page, served from this checkout with hot reload, for a
# reviewer to watch (a Design job's window frames it). HOST is the address to listen on: one the
# reviewer can reach, or 0.0.0.0 for every interface. Needs no app and no server.
HOST ?= 127.0.0.1
PORT ?= 8430
design-preview:
	@echo "design preview: http://$(if $(filter 0.0.0.0,$(HOST)),<this machine's address>,$(HOST)):$(PORT)/gallery.html  (?theme=light, #<section>)"
	GALLERY_HOST=$(HOST) GALLERY_PORT=$(PORT) npx vite --config web/vite.gallery.config.ts

# The same page built to static files (OUT=<dir>; default $$TMPDIR/surface-gallery).
design-gallery:
	$(if $(OUT),GALLERY_OUT=$(OUT)) npx vite build --config web/vite.gallery.config.ts

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
	for a in $(APPS); do find examples/$$a/webui/dist -mindepth 1 ! -name .gitkeep -delete; done
