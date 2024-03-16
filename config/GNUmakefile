ca.key:
	openssl genrsa -out ca.key 4096

ca.crt:
	openssl req -new -x509 -key ca.key -out ca.crt -days 3650 -nodes -subj "/CN=my-self-signed-ca"

tls.crt tls.key:
	openssl req -x509 -CA ca.crt -CAkey ca.key -keyout tls.key -out tls.crt -sha256 -days 3650 -nodes -subj "/CN=mutating-webhook.pod-cpu-booster.svc" -addext "subjectAltName = DNS:mutating-webhook.pod-cpu-booster.svc"

mutating-webhook-certs: ca.key ca.crt tls.crt

build: mutating-webhook-certs
	kustomize build .
