package commands

// RunHello implements the hidden hello verb. Mirrors src/cli/commands/hello.ts.
func RunHello(opts HelloOptions) (int, error) {
	const greeting = "Hello there! At your service."
	if opts.Output.JSONMode() {
		opts.Output.JSON(struct {
			Greeting string `json:"greeting"`
		}{Greeting: greeting})
	} else {
		opts.Output.Print(greeting)
	}
	return 0, nil
}
