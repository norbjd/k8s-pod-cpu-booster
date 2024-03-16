package webhook

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/norbjd/k8s-pod-cpu-booster/pkg/shared"
	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"
)

var deserializer = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()

func admissionReviewFromRequest(r *http.Request, deserializer runtime.Decoder) (*admissionv1.AdmissionReview, error) {
	if r.Header.Get("Content-Type") != "application/json" {
		return nil, fmt.Errorf("expected application/json content-type")
	}

	var body []byte
	if r.Body != nil {
		requestData, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		body = requestData
	}

	admissionReviewRequest := &admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, admissionReviewRequest); err != nil {
		return nil, err
	}

	return admissionReviewRequest, nil
}

func mutatePod(w http.ResponseWriter, r *http.Request) {
	klog.V(9).Infof("received message on mutate")

	admissionReviewRequest, err := admissionReviewFromRequest(r, deserializer)
	if err != nil {
		msg := "error getting admission review from request"
		klog.ErrorS(err, msg)
		w.WriteHeader(400)
		w.Write([]byte(msg))
		return
	}

	// Do server-side validation that we are only dealing with a pod resource. This
	// should also be part of the MutatingWebhookConfiguration in the cluster, but
	// we should verify here before continuing.
	// TODO: also check the label enabling boosting is set
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if admissionReviewRequest.Request.Resource != podResource {
		errNotAPod := fmt.Errorf("did not receive pod, got %s", admissionReviewRequest.Request.Resource.Resource)
		klog.ErrorS(errNotAPod, "")
		w.WriteHeader(400)
		w.Write([]byte(errNotAPod.Error()))
		return
	}

	// Decode the pod from the AdmissionReview.
	rawRequest := admissionReviewRequest.Request.Object.Raw
	pod := corev1.Pod{}
	if _, _, err := deserializer.Decode(rawRequest, nil, &pod); err != nil {
		msg := "error decoding raw pod"
		klog.ErrorS(err, msg)
		w.WriteHeader(500)
		w.Write([]byte(msg))
		return
	}

	boostInfo, err := shared.RetrieveBoostInfo(&pod)
	if err != nil {
		klog.ErrorS(err, "cannot get boost info")
		w.WriteHeader(400)
		w.Write([]byte(err.Error()))
		return
	}

	currentCPURequest := pod.Spec.Containers[boostInfo.ContainerIndex].Resources.Requests.Cpu()
	currentCPULimit := pod.Spec.Containers[boostInfo.ContainerIndex].Resources.Limits.Cpu()

	newCPURequest := resource.NewScaledQuantity(currentCPURequest.ScaledValue(resource.Nano)*int64(boostInfo.Multiplier), resource.Nano)
	newCPULimit := resource.NewScaledQuantity(currentCPULimit.ScaledValue(resource.Nano)*int64(boostInfo.Multiplier), resource.Nano)

	admissionResponse := &admissionv1.AdmissionResponse{}
	patchType := v1.PatchTypeJSONPatch
	patch := fmt.Sprintf(`
		[
			{
				"op": "add",
				"path": "/metadata/labels/norbjd.github.io~1k8s-pod-cpu-booster-progress",
				"value": "boosting"
			},
			{
				"op": "replace",
				"path": "/spec/containers/%d/resources/requests/cpu",
				"value": "%s"
			},
			{
				"op": "replace",
				"path": "/spec/containers/%d/resources/limits/cpu",
				"value": "%s"
			}
		]
	`, boostInfo.ContainerIndex, newCPURequest.String(), boostInfo.ContainerIndex, newCPULimit.String())

	// TODO: in case of a pod from a Deployment or a knative Service, pod.Name is empty, why?
	klog.Infof("Current CPU request/limit for %s/%s (container 0) is %s/%s, will set new CPU limit to %s/%s (boost by %d)",
		pod.Namespace, pod.Name, currentCPURequest, currentCPULimit, newCPURequest, newCPULimit, boostInfo.Multiplier)

	admissionResponse.Allowed = true
	admissionResponse.PatchType = &patchType
	admissionResponse.Patch = []byte(patch)

	// Construct the response, which is just another AdmissionReview.
	var admissionReviewResponse admissionv1.AdmissionReview
	admissionReviewResponse.Response = admissionResponse
	admissionReviewResponse.SetGroupVersionKind(admissionReviewRequest.GroupVersionKind())
	admissionReviewResponse.Response.UID = admissionReviewRequest.Request.UID

	resp, err := json.Marshal(admissionReviewResponse)
	if err != nil {
		msg := "error marshalling response json"
		klog.ErrorS(err, msg)
		w.WriteHeader(500)
		w.Write([]byte(msg))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func Run(port uint, certFile, keyFile string) error {
	cert, errLoadCert := tls.LoadX509KeyPair(certFile, keyFile)
	if errLoadCert != nil {
		return errLoadCert
	}

	klog.Info("Starting webhook server")
	http.HandleFunc("/mutate", mutatePod)
	server := http.Server{
		Addr: fmt.Sprintf(":%d", port),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
		ErrorLog: klog.NewStandardLogger("INFO"), // TODO?
	}

	if err := server.ListenAndServeTLS("", ""); err != nil {
		return err
	}

	return nil
}
