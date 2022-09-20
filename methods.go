package discobolt

import "github.com/gorilla/websocket"

// TODO: add all http routes

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
