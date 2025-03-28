package clients

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
	"github.com/redhatinsights/edge-api/config"
	"github.com/redhatinsights/edge-api/pkg/metrics"
	"github.com/sirupsen/logrus"
)

// HTTPRequestDoer is an interface for HTTP request doer. This interface is missing
// in the standard library and is used to abstract HTTP client creation.
type HTTPRequestDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerErr is a simple wrapped error without any message. Additional message would
// stack for each request as multiple doers are called leading to:
//
// "error in doer1: error in doer2: error in doer3: something happened"
type DoerErr struct {
	Err error
}

func NewDoerErr(err error) *DoerErr {
	return &DoerErr{Err: err}
}

func (e *DoerErr) Error() string {
	return e.Err.Error()
}

func (e *DoerErr) Unwrap() error {
	return e.Err
}

// Shared HTTP transport for all platform clients to utilize connection caching
var transport = &http.Transport{}

func stringToURL(urlStr string) *url.URL {
	if urlStr == "" {
		return nil
	}
	urlProxy, err := url.Parse(urlStr)
	if err != nil {
		return nil
	}
	return urlProxy
}

// NewPlatformClient returns new HTTP client (doer) with optional proxy support and
// additional logging or tracing. Tracing is only available in local setup (when LOCAL
// environment variable is set to true).
func NewPlatformClient(ctx context.Context, proxy string) HTTPRequestDoer {
	var rt http.RoundTripper = transport

	if proxy != "" {
		if clowder.IsClowderEnabled() {
			logrus.WithContext(ctx).Warnf("Unable to use HTTP client proxy in clowder environment: %s", proxy)
		} else {
			logrus.WithContext(ctx).Debugf("Creating HTTP client with proxy %s", proxy)
			rt = &http.Transport{Proxy: http.ProxyURL(stringToURL(proxy))}
		}
	}

	var doer HTTPRequestDoer = ConfigureClientWithTLS(&http.Client{Transport: rt})

	if logrus.IsLevelEnabled(logrus.TraceLevel) && config.Get().Local {
		doer = &LoggingDoer{
			ctx:  ctx,
			doer: doer,
		}
	}

	doer = &MetricsDoer{
		doer: doer,
	}

	return doer
}

// LoggingDoer is a simple HTTP doer that logs request and response data. It is only
// used in TRACE level mode.
type LoggingDoer struct {
	ctx  context.Context
	doer HTTPRequestDoer
}

func (d *LoggingDoer) Do(req *http.Request) (*http.Response, error) {
	// common log data
	log := logrus.WithContext(d.ctx).WithFields(logrus.Fields{
		"method":          req.Method,
		"url":             req.URL.RequestURI(),
		"content_length":  req.ContentLength,
		"platform_client": true,
		"headers":         req.Header,
	})

	// log request
	if req.Body != nil && logrus.IsLevelEnabled(logrus.TraceLevel) {
		// read request data into a byte slice
		requestData, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("cannot read request data: %w", err)
		}

		// rewind the original request reader
		req.Body = io.NopCloser(bytes.NewReader(requestData))

		// log the request data
		log.Trace(bytes.NewBuffer(requestData).String())
	} else {
		log.Tracef("Platform request with no body: %s", req.URL.RequestURI())
	}

	// delegate the request
	resp, doerErr := d.doer.Do(req)

	// log response
	if resp != nil && resp.Body != nil && logrus.IsLevelEnabled(logrus.TraceLevel) {
		// read response data into a byte slice
		responseData, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("cannot read response data: %w", err)
		}

		// rewind the original response reader
		resp.Body = io.NopCloser(bytes.NewReader(responseData))

		// log the response data
		log.Trace(bytes.NewBuffer(responseData).String())
	} else {
		log.Tracef("Platform response with no body: %s", req.URL.RequestURI())
	}

	if doerErr != nil {
		return nil, NewDoerErr(doerErr)
	}

	return resp, nil
}

type MetricsDoer struct {
	doer HTTPRequestDoer
}

func (d *MetricsDoer) Do(req *http.Request) (*http.Response, error) {
	startTime := time.Now()

	resp, doerErr := d.doer.Do(req)

	code := "5xx"
	if resp != nil {
		code = strconv.Itoa(resp.StatusCode/100) + "xx"

		if code != "2xx" {
			logrus.WithContext(req.Context()).WithField("status_code", resp.StatusCode).
				Warnf("Platform request unexpected status %d: %s %s", resp.StatusCode, req.Method, req.URL.RequestURI())
		}
	}

	metrics.PlatformClientDuration.WithLabelValues(req.Method, code).
		Observe(float64(time.Since(startTime).Milliseconds()))

	if doerErr != nil {
		return nil, NewDoerErr(doerErr)
	}

	return resp, nil
}
