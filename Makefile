# Kubrain Yandex Cloud Terraform provider.
#
#   make build              # local binary for dev_overrides
#   make test               # go test ./...
#   make release VERSION=v0.1.0   # cross-compile per OS/arch + upload via gh

NAME    := terraform-provider-yandex
TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: build test release

build:
	go build -o $(NAME) .

test:
	go test ./...

## release: build one binary per OS/arch and attach them to a GitHub release.
## Requires VERSION (e.g. v0.1.0) and an authenticated `gh`.
release:
	@test -n "$(VERSION)" || { echo "set VERSION, e.g. make release VERSION=v0.1.0"; exit 1; }
	gh release create "$(VERSION)" --title "$(VERSION)" --notes "Kubrain Yandex Cloud Terraform provider $(VERSION)"
	@for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; ext=""; \
		[ "$$os" = windows ] && ext=".exe"; \
		out="$(NAME)_$${os}_$${arch}$${ext}"; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "-X main.version=$(VERSION:v%=%)" -o "$$out" . || exit 1; \
		gh release upload "$(VERSION)" "$$out" || exit 1; \
	done
	@echo "released $(VERSION)"
