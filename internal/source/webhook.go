package source

type Webhook struct {
	Method  string
	Path    string
	Handler func() error
}
