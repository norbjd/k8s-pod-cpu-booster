# namespace is used in the certificate's CN and SAN
NAMESPACE := $(shell grep 'namespace: ' kustomization.yaml | cut -d ' ' -f 2)

mutating-webhook-certs:
	openssl genrsa -out ca.key 4096
	openssl req -new -x509 -key ca.key -out ca.crt -days 3650 -nodes -subj "/CN=my-self-signed-ca"
	openssl req -x509 -CA ca.crt -CAkey ca.key -keyout tls.key -out tls.crt -sha256 -days 3650 -nodes -subj "/CN=mutating-webhook.${NAMESPACE}.svc" -addext "subjectAltName = DNS:mutating-webhook.${NAMESPACE}.svc"

remove-certs:
	rm -f *.key *.crt

build:
	kustomize build .
