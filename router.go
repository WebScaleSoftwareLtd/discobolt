package discobolt

import (
	"net/http"
	"sort"
)

// handler is used to define the HTTP handler.
type handler struct {
	// check is used to check if the route specified is used by this and consume its part if so.
	// It returns a boolean for if this is for it, the byte slice for the remainder of the path, and
	// any magical value it wishes to pass to the handler (useful if this is a user param).
	check func(path []byte) (bool, []byte, any)

	// execute is used to execute the handler. The any is from the check above.
	execute func(*Context, any)

	// priority is used to define the priority. Routes with the highest priority should be executed first.
	priority int
}

// ErrorHandler is used to used to define the error handler. The any is the error result that should be returned to the user.
type ErrorHandler func(*Context, error) (result any, status int)

// Router is used to define the base router.
type Router struct {
	handlers         []handler
	errHandler       ErrorHandler
	maxBodySize      int
	disableAutoProxy bool
}

// SetMaxBodySize sets the maximum body size for the router. 0 means the default of 2MB.
func (r *Router) SetMaxBodySize(size int) {
	r.maxBodySize = size
}

type routesSorter struct {
	a []handler
}

func (s routesSorter) Len() int {
	return len(s.a)
}

func (s routesSorter) Swap(i, j int) {
	s.a[i], s.a[j] = s.a[j], s.a[i]
}

func (s routesSorter) Less(i, j int) bool {
	return s.a[i].priority > s.a[j].priority
}

func (r *Router) addHandler(h handler) {
	r.handlers = append(r.handlers, h)
	sort.Sort(routesSorter{a: r.handlers})
}

// UserFacingError is used to define a user facing error.
type UserFacingError interface {
	// Status returns the HTTP status code.
	Status() int

	// Body returns the body of the error.
	Body() any
}

// ServeHTTP implements the http.Handler interface.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Turn the path into a byte slice.
	path := []byte(req.URL.Path)

	// Get the context.
	ctx := &Context{
		contextBase: &contextBase{
			Context:  req.Context(),
			req:      req,
			w:        w,
			r:        r,
			consumed: false,
		},
		pathRemainder: path,
	}

	// Go through the handlers in order.
	for _, h := range r.handlers {
		ok, remainder, val := h.check(path)
		if ok {
			// This is the route! Proceed with this.
			ctx.pathRemainder = remainder
			h.execute(ctx, val)
			if ctx.consumed {
				// This route consumed it all.
				return
			}
		}
	}

	// Throw a 404.
	ctx.pathRemainder = path
	ctx.handleError(RouteNotFound)
}

// DisableAutoProxy is used to turn off transforming trusted proxy servers into the real IP.
func (r *Router) DisableAutoProxy() {
	r.disableAutoProxy = true
}

var _ http.Handler = (*Router)(nil)
