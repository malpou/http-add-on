package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/kedacore/http-add-on/interceptor/config"
	"github.com/kedacore/http-add-on/interceptor/handler"
	"github.com/kedacore/http-add-on/pkg/k8s"
	kedanet "github.com/kedacore/http-add-on/pkg/net"
	"github.com/kedacore/http-add-on/pkg/util"
)

type forwardingConfig struct {
	waitTimeout           time.Duration
	respHeaderTimeout     time.Duration
	forceAttemptHTTP2     bool
	maxIdleConns          int
	idleConnTimeout       time.Duration
	tlsHandshakeTimeout   time.Duration
	expectContinueTimeout time.Duration
}

func newForwardingConfigFromTimeouts(t *config.Timeouts) forwardingConfig {
	return forwardingConfig{
		waitTimeout:           t.WorkloadReplicas,
		respHeaderTimeout:     t.ResponseHeader,
		forceAttemptHTTP2:     t.ForceHTTP2,
		maxIdleConns:          t.MaxIdleConns,
		idleConnTimeout:       t.IdleConnTimeout,
		tlsHandshakeTimeout:   t.TLSHandshakeTimeout,
		expectContinueTimeout: t.ExpectContinueTimeout,
	}
}

// newForwardingHandler takes in the service URL for the app backend
// and forwards incoming requests to it. Note that it isn't multitenant.
// It's intended to be deployed and scaled alongside the application itself.
//
// fwdSvcURL must have a valid scheme in it. The best way to do this is
// creating a URL with url.Parse("https://...")
func newForwardingHandler(
	lggr logr.Logger,
	dialCtxFunc kedanet.DialContextFunc,
	waitFunc forwardWaitFunc,
	fwdCfg forwardingConfig,
	tlsCfg *tls.Config,
	tracingCfg *config.Tracing,
	placeholderHandler *handler.PlaceholderHandler,
	endpointsCache k8s.EndpointsCache,
) http.Handler {
	roundTripper := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialCtxFunc,
		ForceAttemptHTTP2:     fwdCfg.forceAttemptHTTP2,
		MaxIdleConns:          fwdCfg.maxIdleConns,
		IdleConnTimeout:       fwdCfg.idleConnTimeout,
		TLSHandshakeTimeout:   fwdCfg.tlsHandshakeTimeout,
		ExpectContinueTimeout: fwdCfg.expectContinueTimeout,
		ResponseHeaderTimeout: fwdCfg.respHeaderTimeout,
		TLSClientConfig:       tlsCfg,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var uh *handler.Upstream
		ctx := r.Context()
		httpso := util.HTTPSOFromContext(ctx)

		if placeholderHandler != nil && httpso.Spec.PlaceholderConfig != nil && httpso.Spec.PlaceholderConfig.Enabled {
			endpoints, err := endpointsCache.Get(httpso.GetNamespace(), httpso.Spec.ScaleTargetRef.Service)
			if err == nil && workloadActiveEndpoints(endpoints) == 0 {
				lggr.Info("serving placeholder page immediately",
					"namespace", httpso.GetNamespace(),
					"service", httpso.Spec.ScaleTargetRef.Service,
					"reason", "zero replicas detected")

				if placeholderErr := placeholderHandler.ServePlaceholder(w, r, httpso); placeholderErr != nil {
					lggr.Error(placeholderErr, "failed to serve placeholder page")
					w.WriteHeader(http.StatusBadGateway)
					if _, err := w.Write([]byte("error serving placeholder page")); err != nil {
						lggr.Error(err, "could not write error response to client")
					}
				}
				return
			}
		}

		waitFuncCtx, done := context.WithTimeout(ctx, fwdCfg.waitTimeout)
		defer done()
		isColdStart, err := waitFunc(
			waitFuncCtx,
			httpso.GetNamespace(),
			httpso.Spec.ScaleTargetRef.Service,
		)
		if err != nil {
			lggr.Error(err, "wait function failed, not forwarding request")
			w.WriteHeader(http.StatusBadGateway)
			if _, err := w.Write([]byte(fmt.Sprintf("error on backend (%s)", err))); err != nil {
				lggr.Error(err, "could not write error response to client")
			}
			return
		}
		w.Header().Add("X-KEDA-HTTP-Cold-Start", strconv.FormatBool(isColdStart))

		if tracingCfg.Enabled {
			uh = handler.NewUpstream(otelhttp.NewTransport(roundTripper), tracingCfg)
		} else {
			uh = handler.NewUpstream(roundTripper, &config.Tracing{})
		}
		uh.ServeHTTP(w, r)
	})
}
