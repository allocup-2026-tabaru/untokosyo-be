-include .env
export

REGISTRY := $(GCP_REGION)-docker.pkg.dev/$(GCP_PROJECT_ID)/untokosyo-be
IMAGE    := $(REGISTRY)/server

.PHONY: up down logs build push deploy-vm deploy gen-secret

# ── セットアップ ──────────────────────────────────────────────
gen-secret:
	@SECRET=$$(openssl rand -hex 32) && \
	if [ ! -f .env ]; then cp .env.template .env; fi && \
	sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$$SECRET/" .env && \
	echo "JWT_SECRET を .env に書き込みました"

# ── 開発 ────────────────────────────────────────────────────
up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f

# ── デプロイ ─────────────────────────────────────────────────
build:
	docker build -t $(IMAGE):latest .

push:
	docker push $(IMAGE):latest

deploy-vm:
	gcloud compute ssh $(VM_NAME) \
		--zone=$(GCP_ZONE) --project=$(GCP_PROJECT_ID) \
		--command=" \
			sudo gcloud auth configure-docker $(GCP_REGION)-docker.pkg.dev --quiet && \
			cd /opt/untokosyo-be && \
			sudo docker compose pull && \
			sudo docker compose up -d \
		"

deploy: build push deploy-vm
