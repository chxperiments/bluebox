# bluebox Go SDK

```sh
go get github.com/chxperiments/bluebox/sdk/go
```

```go
c := bluebox.New()
sb := c.Sandbox("agent")
if err := sb.Up(ctx); err != nil {
	return err
}
defer sb.Down(ctx)

r, err := sb.Exec(ctx, bluebox.Cmd("python3", "-c", "print(6*7)"))
fmt.Println(string(r.Stdout), r.ExitCode, r.Duration)

// A fresh microVM per call, from the warm pool when the Bluefile has warm:.
r, err = sb.Run(ctx, bluebox.Sh("pytest -q"))
```

`Exec`, `Run`, `WriteFile`, `ReadFile`, `Up`, `Down`, `Sandboxes`, and on forks
`Fork`, `Diff`, `Apply` and `Discard`, work as in
the [Python SDK](../python/README.md), and so does starting `bluebox serve` on
demand. Errors are `*bluebox.Error`, checked with `IsNotFound` and `IsNotUp`.
Standard library only.
