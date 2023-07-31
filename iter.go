package iter

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

var (
	allMethods = []string{http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace}
	rxPatterns = map[string]*regexp.Regexp{}
)

type key string

func Param(ctx context.Context, param string) string {
	s, ok := ctx.Value(key(param)).(string)
	if !ok {
		return ""
	}
	return s
}

type Mux struct {
	NotFound         http.Handler
	MethodNotAllowed http.Handler
	Options          http.Handler
	routes           *[]route
	middleware       []func(http.Handler) http.Handler
}

func New() *Mux {
	return &Mux{
		NotFound: http.NotFoundHandler(),
		MethodNotAllowed: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}),
		Options: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		routes: &[]route{},
	}
}

func (m *Mux) Group(fn func(*Mux)) {
	mm := *m
	fn(&mm)
}

func (m *Mux) Handle(pattern string, handler http.Handler, methods ...string) {
	if len(methods) == 0 {
		methods = allMethods
	}
	if contains(methods, http.MethodGet) && !contains(methods, http.MethodHead) {
		methods = append(methods, http.MethodGet)
	}
	for _, method := range methods {
		route := route{
			method:   strings.ToUpper(method),
			segments: strings.Split(pattern, "/"),
			wildcard: strings.HasSuffix(pattern, "/..."),
			handler:  m.wrap(handler),
		}
		*m.routes = append(*m.routes, route)
	}
	for _, segment := range strings.Split(pattern, "/") {
		if strings.HasPrefix(segment, ":") {
			_, rxPattern, containsRx := strings.Cut(segment, "|")
			if containsRx {
				rxPatterns[rxPattern] = regexp.MustCompile(rxPattern)
			}
		}
	}
}

func (m *Mux) HandlerFunc(pattern string, fn http.HandlerFunc, methods ...string) {
	m.Handle(pattern, fn, methods...)
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segments := strings.Split(r.URL.Path, "/")
	allowedMethods := []string{}
	for _, route := range *m.routes {
		ctx, ok := route.match(r.Context(), segments)
		if ok {
			if r.Method == route.method {
				route.handler.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if !contains(allowedMethods, route.method) {
				allowedMethods = append(allowedMethods, route.method)
			}
		}
	}
	if len(allowedMethods) > 0 {
		w.Header().Set("Allow", strings.Join(append(allowedMethods, http.MethodOptions), ","))
		if r.Method == http.MethodOptions {
			m.wrap(m.Options).ServeHTTP(w, r)
		} else {
			m.wrap(m.MethodNotAllowed).ServeHTTP(w, r)
		}
		return
	}
	m.wrap(m.NotFound).ServeHTTP(w, r)
}

func (m *Mux) Use(middleware ...func(http.Handler) http.Handler) {
	m.middleware = append(m.middleware, middleware...)
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

type route struct {
	method   string
	segments []string
	wildcard bool
	handler  http.Handler
}

func (r *route) match(ctx context.Context, urlSegments []string) (context.Context, bool) {
	if !r.wildcard && len(urlSegments) != len(r.segments) {
		return ctx, false
	}
	for i, segment := range r.segments {
		if i > len(urlSegments)-1 {
			return ctx, false
		}
		if segment == "..." {
			ctx = context.WithValue(ctx, key("..."), strings.Join(urlSegments[i:], "/"))
			return ctx, true
		}
		if strings.HasPrefix(segment, ":") {
			k, rxPattern, containsRx := strings.Cut(strings.TrimPrefix(segment, ":"), "|")
			if containsRx {
				if rxPatterns[rxPattern].MatchString(urlSegments[i]) {
					ctx = context.WithValue(ctx, key(k), urlSegments[i])
					continue
				}
			}
			if !containsRx && urlSegments[i] != "" {
				ctx = context.WithValue(ctx, key(k), urlSegments[i])
				continue
			}
			return ctx, false
		}
		if urlSegments[i] != segment {
			return ctx, false
		}
	}
	return ctx, true
}

func (m *Mux) wrap(handler http.Handler) http.Handler {
	for i := len(m.middleware) - 1; i >= 0; i-- {
		handler = m.middleware[i](handler)
	}
	return handler
}
