#!/bin/sh
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$SCRIPT_DIR/.."
K8S="$ROOT/k8s"

# Load .env from project root (single source of truth for all environments)
if [ -f "$ROOT/.env" ]; then
  set -a
  . "$ROOT/.env"
  set +a
else
  echo "WARNING: .env not found at project root — using defaults"
fi

# Apply defaults for any unset variables
API_PORT="${API_PORT:-3001}"
MINIO_PORT="${MINIO_PORT:-9000}"
MINIO_CONSOLE_PORT="${MINIO_CONSOLE_PORT:-9001}"
MINIO_USE_SSL="${MINIO_USE_SSL:-false}"
MINIO_BUCKET="${MINIO_BUCKET:-arteci}"
MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin}"
OTEL_SERVICE_NAME="${OTEL_SERVICE_NAME:-arteci-api-go}"
OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://signoz-otel-collector.monitoring.svc.cluster.local:4317}"
export API_PORT MINIO_PORT MINIO_CONSOLE_PORT MINIO_USE_SSL MINIO_BUCKET OTEL_SERVICE_NAME OTEL_EXPORTER_OTLP_ENDPOINT

export KUBECONFIG="$SCRIPT_DIR/kubeconfig.yaml"

# Always fetch fresh kubeconfig: vagrant up regenerates the k3s CA each time,
# so a stale kubeconfig causes "certificate signed by unknown authority" errors.
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
if helm status signoz -n monitoring > /dev/null 2>&1; then
  echo "    SigNoz already installed, skipping"
else
  helm install signoz signoz/signoz \
    -n monitoring \
    -f "$K8S/signoz/helm-values.yaml" \
    --timeout 10m \
    --wait
fi

echo ""
echo "==> Applying arteci manifests..."
kubectl apply -f "$K8S/namespace.yaml"

# Generate secret from .env — never read from a committed file
kubectl create secret generic arteci-api-secret \
  --from-literal=MINIO_ACCESS_KEY="${MINIO_ROOT_USER}" \
  --from-literal=MINIO_SECRET_KEY="${MINIO_ROOT_PASSWORD}" \
  --from-literal=SIGNOZ_INGESTION_KEY="${SIGNOZ_INGESTION_KEY:-}" \
  --namespace=arteci \
  --dry-run=client -o yaml | kubectl apply -f -

# Apply configmap and deployment with env substitution
envsubst '${API_PORT} ${MINIO_PORT} ${MINIO_USE_SSL} ${MINIO_BUCKET} ${OTEL_SERVICE_NAME} ${OTEL_EXPORTER_OTLP_ENDPOINT}' \
  < "$K8S/go/api-configmap.yaml" | kubectl apply -f -

envsubst '${API_PORT}' \
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
echo "==> Stack ready."
echo "    API    : curl http://localhost:${API_PORT}/health"
echo "    SigNoz : kubectl port-forward svc/signoz 8080:8080 -n monitoring"
