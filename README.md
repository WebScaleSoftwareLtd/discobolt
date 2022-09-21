# Discobolt

[GoDoc Reference](https://pkg.go.dev/github.com/webscalesoftwareltd/discobolt)

**Discobolt is under active development. Testing is not complete and this is not battle tested yet. Quality of life fixes, tests, and documentation generation are works in progress!**

Discobolt is a Go library that implements `http.Handler` to make building web applications as easy as building functions with generics. It has the following features:
- **Clear Routing:** The actual routing is designed to be done in one nested place. This makes it clear during refactoring where everything in the app actually is. If you need to add a bunch of routes under one context, you can simply call a function.
- **Incredible Content Type Support:** See [input and output types](#input-and-output-types) for more information, but our list is extensive and supports things like HTML to allow you to write your whole site within Discobolt.
- **Keeps It Simple:** Discobolt is incredibly simple to write functions for. The API functions are generic and are designed to be added inside the context `discobolt.GET(ctx, func() (T, error) {...})`. The type returned is turned into the content types listed below, and errors are automatically passed to the error handler (unless they implement the `UserFacingError` interface, then it is returned to the user).

## Input and Output types

Discobolt supports the following input and output types. This is done via the `Accept` header for outputs and `Content-Type` for outputs:

- `application/json` (JSON)
- `application/msgpack` or `application/x-msgpack` (msgpack, uses JSON tags)
- `application/yaml` or `text/yaml` (YAML)
- `application/xml` or `text/xml` (XML)
- `text/plain` (text, return content type only, only allowed if `String() string` is on the returned interface)
- `text/html` (HTML, return content type only, only allowed if `HTML() ([]byte, error)` is on the returned interface)
- `application/x-www-form-urlencoded` (form, input content type only)
- `multipart/form-data` (form, input content type only)

If `Content-Type` is not specified, Discobolt will default to `application/json`. If `Accept` is not specified, Discobolt will initially try to default to `Content-Type`. If both are blank or nothing in the `Accept` header is supported, Discobolt will use `application/json`.

## Getting started
To get started, simply install Discobolt with `go get github.com/webscalesoftwareltd/discobolt`. Then, you can start writing your first Discobolt app. First you will want to make the route:

```go
router := &discobolt.Router{}
```

From here, you will want to use matchers to go ahead and match the route you want. The matcher can be used on the router or the context object, and returns a function with a context parameter. This context can have additional matchers attached to it or you can attach a HTTP method. The following matchers are supported:
- `Static`: Matches a static string until the next slash after the part. This is useful for general routing (for example, you'll probably want a matcher for `api` and then a matcher inside that for `v1`). As a special case, a blank string here can be used to attach to the root.
- `Int`: Matches a valid integer. Returns a int alongside the context.
- `Uint`: Matches a valid unsigned integer. Returns a uint alongside the context.
- `Float`: Matches a valid float. Returns a float64 alongside the context.
- `String`: Matches a valid string. Returns a string alongside the context. The path part cannot be blank for this to match. The string is automatically unescaped.

From here, inside the matcher you wish to use for a route (or parents of it, you are not limited to a static or dynamic param, it can fallback), you can go ahead and do one of the following:
- **Add a HTTP method:** Using `discobolt.<method>`, you can go ahead and add the HTTP logic you want in this by adding a function with the signature `func() (T, error)`. The type returned will be transformed as per the content type information [above](#input-and-output-types). See [error handling](#error-handling) for information on how errors are processed.
- **Add a WebSocket handler:** Using `discobolt.WebSocket(*Context, *websocket.Upgrader, func(*websocket.Conn) error)`, you can go ahead and add a WebSocket handler. The function is called with the upgraded connection if successful and this is a upgrade request. Errors will go to the [error handler](#error-handling) but any results will not be sent to the user.

For example, if you wanted to match `/api/v1/hello/:name`, you would do the following:

```go
discobolt.Static(router, "api", func(ctx *discobolt.Context) {
	// The API v1 functionality. You'd likely want this function elsewhere in the real world.
	discobolt.Static(ctx, "v1", func(ctx *discobolt.Context) {
		// The hello function. You'd likely want this function elsewhere in the real world.
		discobolt.Static(ctx, "hello", func(ctx *discobolt.Context) {
			ctx.String(ctx, func(ctx *discobolt.Context, name string) {
				ctx.GET(func() (string, error) {
					return "Hello, " + name, nil
				})
			})
		})
	})
})
```

From here, you can go ahead and start the server:

```go
if err := http.ListenAndServe(":8080", router); err != nil {
    panic(err)
}
```

You will then likely want to [add a custom error handler](#error-handling) and [parse bodies/query strings](#http-bodiesqueries).

## HTTP bodies/queries
To parse query params/HTTP bodies, you can first make a struct that accepts the input types listed above:
```go
type HelloWorldInputs struct {
    Name string `json:"name" form:"name" query:"name" xml:"name" yaml:"name"`
}
```

From here, you can simply add a pointer to it after the function. If it is a GET request, this will only transform query parameters, but other methods will use the input types listed above:
```go
...
var input HelloWorldInputs
discobolt.GET(ctx, func() (T, error) {...}, &input)
...
```
If this fails, it will be caught by the [error handler](#error-handling) wrapped by a bad request type. You can use `IsBadRequest(err)` to check if it is a bad request error.

## Custom checks
Inside a HTTP router, you may desire to add a check. The role of a check is to allow you to check something before executing any methods on the current matcher or any matcher afterwards. This can be done with the `AddCheck` function:
```go
func checkUserAuth(ctx *discobolt.Context, user *User) func() error {
	return func() error {
		auth := ctx.RequestHeaders().Get("Authorization")
		// TODO: Function content here!
		return errors.New("not authenticated!")
	}
}

...

discobolt.Static(router, "api", func(ctx *discobolt.Context) {
	discobolt.Static(ctx, "v1", func(ctx *discobolt.Context) {
		// Add a check to make sure the user is logged in.
		var user User
		discobolt.AddCheck(ctx, checkUserAuth(ctx, &user))

		// By the time we get into a route method, we can assume the method is done. However, you cannot
		// assume that the function will execute immediately, so always pass a pointer.
		discobolt.Static(ctx, "@me", func(ctx *discobolt.Context) {
			discobolt.GET(ctx, func() (*User, error) {
				return &user, nil
			})
		})
	})
})
```

## Error handling
Any errors returned here will be given to the error handler unless they implement `UserFacingError`. The idea of this interface is that you implement a standardised error for this:
```go
type userError struct {
	status int

	Message string `json:"message" xml:"message" yaml:"message"`
}

// Status defines the HTTP status code to return to the user.
func (e userError) Status() int {
	return e.status
}

// Body is the body to return to the user. The type is handled by the content type handlers above.
func (e userError) Body() any {
	return e
}

// Error implements the error interface.
func (e userError) Error() string {
	return e.Message
}

// String allows this to be returned for text/plain.
func (e userError) String() string {
	return e.Message
}

// HTML allows this to be returned for text/html.
func (e userError) HTML() ([]byte, error) {
	return []byte(`<h1>Request Error</h1>
<p>` + html.EscapeString(e.Message) + "</p>"), nil
}
```

The error handler by default is very basic. It returns the following:
- **Error is bad request:** Return status 400 along with a body in the format {message => Bad Request}.
- **Error is route not found:** Return status 404 along with a body in the format {message => Not Found}.
- **Error is something not user facing:** Return status 500 along with a body in the format {message => Internal Server Error}.

You likely want to change this. To do this, we can call `SetErrorHandler` on the router:
```go
router.SetErrorHandler(func(ctx *Context, err error) (result any, status int) {
	if discobolt.IsBadRequest(err) {
		return "something went wrong", 400
	}

	if errors.Is(err, discobolt.ErrRouteNotFound) {
		return "not found", 404
	}

	// TODO: something else here!
	return "something went wrong", 500
})
```

The body that is sent is converted to the content type that the user requested. If the user did not request a content type, it will be sent as JSON as per above.

## Redirects

Redirects are done by returning the `discobolt.Redirect` struct either as a error or the result. Discobolt will automatically redirect to the content following the struct contents.

Redirects cannot be nil pointers.
