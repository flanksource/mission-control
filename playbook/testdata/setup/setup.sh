set -e

kubectl create secret generic gemini-token-secret \
  --from-literal=TOKEN=${GITHUB_TOKEN}

kubectl get secrets -A


echo "setup artifact store"
mkdir -p .artifacts

kubectl apply -f ./playbook/testdata/setup/artifact.yaml