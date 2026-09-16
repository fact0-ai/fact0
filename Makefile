.PHONY: init up down logs owner reset-password test-api test-web test-sdk docs-sync
init:
	python3 scripts/init-env.py
up:
	docker compose up --build -d --wait
down:
	docker compose down
logs:
	docker compose logs --tail=100 -f
owner:
	docker compose exec web node scripts/owner.mjs create
reset-password:
	docker compose exec web node scripts/owner.mjs reset-password
test-api:
	cd api && go vet ./... && go test -race ./...
test-web:
	cd web && npm run lint && npm test && npx tsc --noEmit && npm run build
test-sdk:
	cd sdk/python && python -m pytest -q
	cd sdk/go && go test -race ./...
	cd sdk/typescript && npm test && npm run build
docs-sync:
	bash scripts/sync-openapi.sh
