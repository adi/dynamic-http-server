package dynhttpsrv

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

type swappableRouter struct {
	mu     *sync.Mutex
	router *mux.Router
}

func createSwappableRouter(router *mux.Router) *swappableRouter {
	return &swappableRouter{
		mu:     &sync.Mutex{},
		router: router,
	}
}

func (sr *swappableRouter) swap(newRouter *mux.Router) {
	sr.mu.Lock()
	sr.router = newRouter
	sr.mu.Unlock()
}

func (sr swappableRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sr.mu.Lock()
	router := sr.router
	sr.mu.Unlock()
	router.ServeHTTP(w, r)
}

type Endpoint struct {
	Methods []string
	Paths   []string
	Handler func(res http.ResponseWriter, req *http.Request)
}

type DynHttpSrv struct {
	Router    *swappableRouter
	Endpoints []*Endpoint
}

// New creates a new dynamic HTTP server listening on address and obeying cancelling through ctx
func New(ctx context.Context, addr string) *DynHttpSrv {
	srvMux := createSwappableRouter(mux.NewRouter().StrictSlash(true))
	srv := &http.Server{
		Addr:    addr,
		Handler: srvMux,
	}

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("ListenAndServe ended with error: %v\n", err)
		}
		log.Printf("ListenAndServe exited\n")
	}()

	go func() {
		for {
			if ctx.Err() == context.Canceled {
				srv.Shutdown(context.Background())
				break
			}
			time.Sleep(1 * time.Second)
		}
	}()

	return &DynHttpSrv{
		Router:    srvMux,
		Endpoints: make([]*Endpoint, 0),
	}
}

func (dhs *DynHttpSrv) AddEndpoint(endpoint *Endpoint) error {
	pos := -1
	for i, existingEndpoint := range dhs.Endpoints {
		if existingEndpoint == endpoint {
			pos = i
			break
		}
	}
	if pos != -1 {
		return errors.New("endpoint already added")
	}
	dhs.Endpoints = append(dhs.Endpoints, endpoint)
	dhs.reloadEndpoints()
	return nil
}

func (dhs *DynHttpSrv) DelEndpoint(endpoint *Endpoint) error {
	pos := -1
	for i, existingEndpoint := range dhs.Endpoints {
		if existingEndpoint == endpoint {
			pos = i
			break
		}
	}
	if pos == -1 {
		return errors.New("endpoint not found")
	}
	dhs.Endpoints = append(dhs.Endpoints[0:pos], dhs.Endpoints[pos+1:]...)
	dhs.reloadEndpoints()
	return nil
}

func (dhs *DynHttpSrv) reloadEndpoints() {
	newRouter := mux.NewRouter().StrictSlash(true)
	for _, endpoint := range dhs.Endpoints {
		if endpoint.Paths == nil {
			if endpoint.Methods == nil {
				newRouter.PathPrefix("/").HandlerFunc(endpoint.Handler)
			} else {
				newRouter.PathPrefix("/").HandlerFunc(endpoint.Handler).Methods(endpoint.Methods...)
			}
		} else {
			for _, path := range endpoint.Paths {
				if endpoint.Methods == nil {
					newRouter.HandleFunc(path, endpoint.Handler)
				} else {
					newRouter.HandleFunc(path, endpoint.Handler).Methods(endpoint.Methods...)
				}
			}
		}
	}
	dhs.Router.swap(newRouter)
}
