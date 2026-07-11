#!/bin/sh
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K8S="$SCRIPT_DIR/../k8s"

export KUBECONFIG="$SCRIPT_DIR/kubeconfig.yaml"

if [ ! -f "$KUBECONFIG" ]; then
  echo "kubeconfig not found — copying from VM..."
  vagrant -C "$SCRIPT_DIR" ssh -c "cat /home/vagrant/kubeconfig.yaml" > "$KUBECONFIG"
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
kubectl apply -f "$K8S/go/api-secret.yaml"
kubectl apply -f "$K8S/go/api-configmap.yaml"
kubectl apply -f "$K8S/minio/minio-deployment.yaml"
kubectl apply -f "$K8S/minio/minio-service.yaml"
kubectl apply -f "$K8S/minio/fixtures-configmap.yaml"
kubectl apply -f "$K8S/minio/minio-init-job.yaml"
kubectl apply -f "$K8S/go/api-deployment.yaml"
kubectl apply -f "$K8S/go/api-service.yaml"

echo ""
echo "==> Waiting for MinIO..."
kubectl rollout status deployment/minio -n arteci --timeout=120s

echo "==> Waiting for minio-init job..."
kubectl wait --for=condition=complete job/minio-init -n arteci --timeout=120s

echo "==> Waiting for API..."
kubectl rollout status deployment/arteci-api -n arteci --timeout=120s

echo ""
echo "==> Stack ready."
echo "    API    : curl http://localhost:3001/health"
echo "    SigNoz : kubectl port-forward svc/signoz-frontend 3301:3301 -n monitoring"
