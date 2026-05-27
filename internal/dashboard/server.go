package dashboard

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewMux(cl client.Client, namespace string) http.Handler {
	h := &Handler{Client: cl, Namespace: namespace}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.HandleList)
	mux.HandleFunc("GET /volumes/{name}", h.HandleDetail)
	mux.HandleFunc("GET /volumes/{name}/overview", h.HandleOverview)
	mux.HandleFunc("GET /volumes/{name}/replicas", h.HandleReplicas)
	mux.HandleFunc("GET /volumes/new", h.HandleCreateForm)
	mux.HandleFunc("POST /volumes/create", h.HandleCreate)
	mux.HandleFunc("GET /volumes/{name}/scale-form", h.HandleScaleForm)
	mux.HandleFunc("POST /volumes/{name}/scale", h.HandleScale)
	mux.HandleFunc("GET /volumes/{name}/delete-form", h.HandleDeleteForm)
	mux.HandleFunc("POST /volumes/{name}/delete", h.HandleDelete)
	mux.HandleFunc("POST /volumes/{name}/replicas/{replica}/delete", h.HandleDeleteReplica)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	return mux
}
