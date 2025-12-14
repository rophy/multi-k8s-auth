.PHONY: build kind deploy test-unit test-e2e test-e2e-local test-e2e-remote test destroy clean help

# Build Docker images
build:
	skaffold build

# Create kind clusters for multi-cluster testing
kind:
	@echo "=========================================="
	@echo "Setting up Kind Clusters"
	@echo "=========================================="
	@./scripts/setup-kind-clusters.sh

# Deploy to multi-cluster environment
# Flow: create clusters → deploy cluster-b → get creds → deploy cluster-a
deploy:
	@echo "=========================================="
	@echo "Deploying to Multi-Cluster Environment"
	@echo "=========================================="
	@echo ""
	@echo "Step 1: Ensuring kind clusters exist..."
	@./scripts/setup-kind-clusters.sh
	@echo ""
	@echo "Step 2: Building images..."
	@skaffold build --kube-context kind-cluster-a
	@echo ""
	@echo "Step 3: Deploying to cluster-b first..."
	@docker tag $$(docker images --format '{{.Repository}}:{{.Tag}}' | grep '^multi-k8s-auth-test:' | head -1) multi-k8s-auth-test:latest
	@kind load docker-image multi-k8s-auth-test:latest --name cluster-b
	@kubectl config use-context kind-cluster-b
	@kubectl apply -f k8s/cluster-b/
	@echo "Waiting for test-client in cluster-b to be ready..."
	@kubectl wait --for=condition=ready pod -l app=test-client -n multi-k8s-auth --timeout=120s
	@echo ""
	@echo "Step 4: Extracting cluster-b credentials and configuring cluster-a..."
	@./scripts/setup-multicluster.sh
	@echo ""
	@echo "Step 5: Deploying to cluster-a..."
	@kubectl config use-context kind-cluster-a
	@skaffold run -m multi-k8s-auth --kube-context kind-cluster-a
	@echo ""
	@echo "✅ Deployment complete!"
	@echo ""
	@echo "Next: Run 'make test' to verify authentication"

# Run unit tests
test-unit:
	go test -v ./internal/...

# Run e2e tests in cluster-a
test-e2e-local:
	@echo "Running e2e tests in cluster-a..."
	@kubectl config use-context kind-cluster-a
	kubectl exec -n multi-k8s-auth deployment/test-client -- go test -v ./test/e2e/...

# Run e2e tests in cluster-b (cross-cluster)
test-e2e-remote:
	@echo "Running e2e tests in cluster-b..."
	@kubectl config use-context kind-cluster-b
	kubectl exec -n multi-k8s-auth deployment/test-client -- go test -v ./test/e2e/...

# Run all e2e tests
test-e2e: test-e2e-local test-e2e-remote

# Run all tests
test: test-unit test-e2e

# Destroy kind clusters and all deployments
destroy:
	@echo "=========================================="
	@echo "Destroying Multi-Cluster Environment"
	@echo "=========================================="
	@echo ""
	@echo "Removing deployments from cluster-a..."
	@kubectl config use-context kind-cluster-a 2>/dev/null && skaffold delete || echo "Cluster-a not found or already cleaned"
	@echo ""
	@echo "Removing deployments from cluster-b..."
	@kubectl config use-context kind-cluster-b 2>/dev/null && kubectl delete namespace multi-k8s-auth --ignore-not-found || echo "Cluster-b not found or already cleaned"
	@echo ""
	@echo "Deleting kind clusters..."
	@kind delete cluster --name cluster-a 2>/dev/null || echo "Cluster-a already deleted"
	@kind delete cluster --name cluster-b 2>/dev/null || echo "Cluster-b already deleted"
	@echo ""
	@echo "Destroy complete!"

# Clean local build artifacts
clean:
	rm -rf bin/

# Show help
help:
	@echo "multi-k8s-auth - Cross-Cluster Kubernetes Authentication"
	@echo ""
	@echo "Build targets:"
	@echo "  make build             - Build Docker images"
	@echo "  make clean             - Clean local build artifacts"
	@echo ""
	@echo "Multi-cluster environment:"
	@echo "  make kind              - Create two kind clusters (cluster-a, cluster-b)"
	@echo "  make deploy            - Setup clusters and deploy everything"
	@echo "  make test              - Run all tests (unit + e2e)"
	@echo "  make test-unit         - Run unit tests only"
	@echo "  make test-e2e          - Run e2e tests in both clusters"
	@echo "  make test-e2e-local    - Run e2e tests in cluster-a only"
	@echo "  make test-e2e-remote   - Run e2e tests in cluster-b only"
	@echo "  make destroy           - Destroy everything (deployments + clusters)"
	@echo ""
	@echo "Workflow:"
	@echo "  1. make deploy         - Create clusters and deploy service"
	@echo "  2. make test           - Run authentication tests"
	@echo "  3. make destroy        - Destroy clusters when done"
	@echo ""
	@echo "Quick start:"
	@echo "  make deploy && make test"
	@echo ""
	@echo "Architecture:"
	@echo "  - cluster-a: multi-k8s-auth service + test-client"
	@echo "  - cluster-b: test-client (validates tokens against cluster-a service)"
	@echo ""
