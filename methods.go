package discobolt

import "github.com/gorilla/websocket"

// GET is used to define a GET request in the current route context.
func GET[T any](c *Context, handler func() (T, error), inputs ...any) {
	c.getRunner = func() {
		methodHandler(c, "GET", handler, inputs)
	}
}

// WebSocket is used to define a WebSocket request in the current route context.
func WebSocket(c *Context, upgrader *websocket.Upgrader, handler func(*websocket.Conn) error) {
	c.webSocketUpgrader = upgrader
	c.webSocketHandler = handler
}

// POST is used to define a POST request in the current route context.
func POST[T any](c *Context, handler func() (T, error), inputs ...any) {
	methodHandler(c, "POST", handler, inputs)
}

// PUT is used to define a PUT request in the current route context.
func PUT[T any](c *Context, handler func() (T, error), inputs ...any) {
	methodHandler(c, "PUT", handler, inputs)
}

// DELETE is used to define a DELETE request in the current route context.
func DELETE[T any](c *Context, handler func() (T, error), inputs ...any) {
	methodHandler(c, "DELETE", handler, inputs)
}

// PATCH is used to define a PATCH request in the current route context.
func PATCH[T any](c *Context, handler func() (T, error), inputs ...any) {
	methodHandler(c, "PATCH", handler, inputs)
}

// OPTIONS is used to define a OPTIONS request in the current route context.
func OPTIONS[T any](c *Context, handler func() (T, error), inputs ...any) {
	methodHandler(c, "OPTIONS", handler, inputs)
}
