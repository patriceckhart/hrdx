.PHONY: build frontend check test vet frontend-check clean

WAILS ?= wails

# Full desktop app bundle in build/bin. Requires Wails v2 and bun.
build:
	$(WAILS) build -clean
	@touch frontend/out/.gitkeep

# Static export only; main.go embeds frontend/out.
frontend:
	cd frontend && bun install --frozen-lockfile && bun run build
	@touch frontend/out/.gitkeep

vet:
	go vet ./...

test:
	go test -race ./...

frontend-check:
	cd frontend && bun install --frozen-lockfile && bun run typecheck && bun run lint && bun test && bun run build
	@touch frontend/out/.gitkeep

# The desktop validation ladder. go vet compiles main.go, whose embed
# directive needs frontend/out; .gitkeep keeps the directory present.
check: vet test frontend-check

clean:
	rm -rf build/bin frontend/.next
	find frontend/out -mindepth 1 ! -name .gitkeep -delete
