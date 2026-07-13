#!/bin/sh
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$SCRIPT_DIR/.."
K8S="$ROOT/k8s"

if [ -f "$ROOT/.env" ]; then
  set -a
  . "$ROOT/.env"
  set +a
else
  echo "WARNING: .env not found at project root — using defaults"
fi

API_PORT="${API_PORT:-3001}"
MINIO_PORT="${MINIO_PORT:-9000}"
MINIO_CONSOLE_PORT="${MINIO_CONSOLE_PORT:-9001}"
MINIO_USE_SSL="${MINIO_USE_SSL:-false}"
MINIO_BUCKET="${MINIO_BUCKET:-arteci}"
MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin}"
OTEL_SERVICE_NAME="${OTEL_SERVICE_NAME:-arteci-api-go}"
OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://signoz-otel-collector.monitoring.svc.cluster.local:4317}"
SIGNOZ_POSTGRES_PASSWORD="${SIGNOZ_POSTGRES_PASSWORD:-signoz}"
SIGNOZ_JWT_SECRET="${SIGNOZ_JWT_SECRET:-arteci-signoz-local-secret-key-32chars}"
SIGNOZ_ROOT_EMAIL="${SIGNOZ_ROOT_EMAIL:-admin@arteci.local}"
SIGNOZ_ROOT_PASSWORD="${SIGNOZ_ROOT_PASSWORD:-Arteci-Signoz-Admin-2026}"
SIGNOZ_ROOT_ORG_NAME="${SIGNOZ_ROOT_ORG_NAME:-arteci}"
API_IMAGE_TAG="${API_IMAGE_TAG:-latest}"
export API_PORT MINIO_PORT MINIO_CONSOLE_PORT MINIO_USE_SSL MINIO_BUCKET OTEL_SERVICE_NAME OTEL_EXPORTER_OTLP_ENDPOINT API_IMAGE_TAG

export KUBECONFIG="$SCRIPT_DIR/kubeconfig.yaml"

echo "Fetching kubeconfig from VM..."
VAGRANT_CWD="$SCRIPT_DIR" vagrant ssh -c "cat /home/vagrant/kubeconfig.yaml" > "$KUBECONFIG"
if ! grep -q "apiVersion" "$KUBECONFIG" 2>/dev/null; then
  echo "ERROR: failed to fetch a valid kubeconfig from VM" >&2
  exit 1
fi

echo "==> Installing SigNoz (observability stack)..."
kubectl apply -f "$K8S/signoz/namespace.yaml"
helm repo add signoz https://charts.signoz.io 2>/dev/null || true
helm repo update signoz
# upgrade --install: idempotent, and picks up SIGNOZ_*-from-.env changes on reruns
helm upgrade --install signoz signoz/signoz \
  -n monitoring \
  -f "$K8S/signoz/helm-values.yaml" \
  --set-string "postgresql.auth.password=${SIGNOZ_POSTGRES_PASSWORD}" \
  --set-string "signoz.env.SIGNOZ_TOKENIZER_JWT_SECRET=${SIGNOZ_JWT_SECRET}" \
  --set-string "signoz.env.SIGNOZ_USER_ROOT_EMAIL=${SIGNOZ_ROOT_EMAIL}" \
  --set-string "signoz.env.SIGNOZ_USER_ROOT_PASSWORD=${SIGNOZ_ROOT_PASSWORD}" \
  --set-string "signoz.env.SIGNOZ_USER_ROOT_ORG_NAME=${SIGNOZ_ROOT_ORG_NAME}" \
  --timeout 10m \
  --wait

echo ""
echo "==> Applying arteci manifests..."
kubectl apply -f "$K8S/namespace.yaml"

kubectl create secret generic arteci-api-secret \
  --from-literal=MINIO_ACCESS_KEY="${MINIO_ROOT_USER}" \
  --from-literal=MINIO_SECRET_KEY="${MINIO_ROOT_PASSWORD}" \
  --from-literal=SIGNOZ_INGESTION_KEY="${SIGNOZ_INGESTION_KEY:-}" \
  --namespace=arteci \
  --dry-run=client -o yaml | kubectl apply -f -

envsubst '${API_PORT} ${MINIO_PORT} ${MINIO_USE_SSL} ${MINIO_BUCKET} ${OTEL_SERVICE_NAME} ${OTEL_EXPORTER_OTLP_ENDPOINT}' \
  < "$K8S/go/api-configmap.yaml" | kubectl apply -f -

envsubst '${API_PORT} ${API_IMAGE_TAG}' \
  < "$K8S/go/api-deployment.yaml" | kubectl apply -f -

MINIO_VARS='${MINIO_PORT} ${MINIO_CONSOLE_PORT} ${MINIO_BUCKET}'
envsubst "$MINIO_VARS" < "$K8S/minio/minio-deployment.yaml" | kubectl apply -f -
envsubst "$MINIO_VARS" < "$K8S/minio/minio-service.yaml"    | kubectl apply -f -
kubectl apply -f "$K8S/minio/fixtures-configmap.yaml"
envsubst "$MINIO_VARS" < "$K8S/minio/minio-init-job.yaml"   | kubectl apply -f -
envsubst '${API_PORT}' < "$K8S/go/api-service.yaml" | kubectl apply -f -

echo ""
echo "==> Waiting for MinIO..."
kubectl rollout status deployment/minio -n arteci --timeout=120s

echo "==> Waiting for minio-init job..."
kubectl wait --for=condition=complete job/minio-init -n arteci --timeout=120s

echo "==> Waiting for API..."
kubectl rollout status deployment/arteci-api -n arteci --timeout=120s

echo ""
echo "==> Stack ready — accessible directly, no port-forward needed:"
echo "    API           : curl http://localhost:${API_PORT}/health"
echo "    MinIO console : http://localhost:${MINIO_CONSOLE_PORT}"
echo "    SigNoz        : http://localhost:8080"
