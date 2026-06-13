.PHONY: images copy-data deploy-infra deploy-app deploy import status logs clean

# ── Build images inside minikube's Docker daemon ──────────────────────────────
images:
	@echo "→ Pointing Docker to minikube..."
	eval $$(minikube docker-env) && \
	docker build -f docker/api.Dockerfile         -t geolink-api:latest         . && \
	docker build -f docker/bulkimport.Dockerfile  -t geolink-bulkimport:latest  . && \
	docker build -f docker/personalizer.Dockerfile -t geolink-personalizer:latest .
	@echo "✓ Images built inside minikube"

# ── Copy GeoNames data file to minikube node (avoids 1.7GB re-download) ──────
copy-data:
	@echo "→ Copying data/allCountries.txt to minikube node at /tmp/geonames/..."
	minikube ssh "sudo mkdir -p /tmp/geonames"
	minikube cp data/allCountries.txt /tmp/geonames/allCountries.txt
	@echo "✓ Data file copied"

# ── Apply infra (postgres, redis, kafka, typesense) ───────────────────────────
deploy-infra:
	kubectl apply -f k8s/namespace.yaml
	kubectl apply -f k8s/secrets.yaml
	kubectl apply -f k8s/configmap.yaml
	kubectl apply -f k8s/infra/postgres.yaml
	kubectl apply -f k8s/infra/redis.yaml
	kubectl apply -f k8s/infra/kafka.yaml
	kubectl apply -f k8s/infra/typesense.yaml
	@echo "→ Waiting for infra pods to be ready..."
	kubectl rollout status statefulset/postgres   -n geolink --timeout=120s
	kubectl rollout status deployment/redis       -n geolink --timeout=60s
	kubectl rollout status statefulset/kafka      -n geolink --timeout=180s
	kubectl rollout status statefulset/typesense  -n geolink --timeout=120s
	@echo "✓ Infra ready"

# ── Deploy app services ───────────────────────────────────────────────────────
deploy-app:
	kubectl apply -f k8s/app/api.yaml
	kubectl apply -f k8s/app/personalizer.yaml
	kubectl rollout status deployment/api         -n geolink --timeout=120s
	kubectl rollout status deployment/personalizer -n geolink --timeout=60s
	@echo "✓ App deployed"

# ── Full deploy (infra + app, no import) ──────────────────────────────────────
deploy: deploy-infra deploy-app

# ── Run bulkimport Job (one-shot, ~8 min for 12M records) ────────────────────
import:
	kubectl apply -f k8s/app/bulkimport.yaml
	@echo "→ Following import logs (Ctrl-C to detach, job keeps running)..."
	kubectl wait --for=condition=ready pod -l job-name=bulkimport -n geolink --timeout=60s
	kubectl logs -f job/bulkimport -n geolink

# ── Status ────────────────────────────────────────────────────────────────────
status:
	kubectl get pods -n geolink -o wide

# ── Logs ─────────────────────────────────────────────────────────────────────
logs-api:
	kubectl logs -f deployment/api -n geolink

logs-personalizer:
	kubectl logs -f deployment/personalizer -n geolink

logs-kafka:
	kubectl logs -f statefulset/kafka -n geolink

# ── Open API in browser via minikube ─────────────────────────────────────────
open:
	minikube service api -n geolink

# ── Destroy everything ────────────────────────────────────────────────────────
clean:
	kubectl delete namespace geolink --ignore-not-found=true
	kubectl delete pv geonames-pv --ignore-not-found=true
	@echo "✓ All geolink resources deleted"
