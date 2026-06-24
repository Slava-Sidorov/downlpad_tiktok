package cli

import (
	"flag"
	"strconv"
	"time"
)

type Flag interface{ apply(*flag.FlagSet, *Context) }

type App struct {
	Name     string
	Usage    string
	Commands []*Command
	Flags    []Flag
	Action   func(*Context) error
}

type Command struct {
	Name   string
	Usage  string
	Action func(*Context) error
}

type Context struct {
	strings   map[string]*string
	ints      map[string]*int
	float64s  map[string]*float64
	durations map[string]*time.Duration
}

func (a *App) Run(args []string) error {
	if len(args) > 1 {
		for _, command := range a.Commands {
			if command != nil && command.Name == args[1] {
				if command.Action == nil {
					return nil
				}
				return command.Action(newContext())
			}
		}
	}

	ctx := newContext()
	fs := flag.NewFlagSet(a.Name, flag.ExitOnError)
	fs.Usage = func() {}
	for _, f := range a.Flags {
		f.apply(fs, ctx)
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if a.Action == nil {
		return nil
	}
	return a.Action(ctx)
}

func newContext() *Context {
	return &Context{
		strings:   map[string]*string{},
		ints:      map[string]*int{},
		float64s:  map[string]*float64{},
		durations: map[string]*time.Duration{},
	}
}

func (c *Context) String(name string) string {
	if v, ok := c.strings[name]; ok && v != nil {
		return *v
	}
	return ""
}
func (c *Context) Int(name string) int {
	if v, ok := c.ints[name]; ok && v != nil {
		return *v
	}
	return 0
}
func (c *Context) Float64(name string) float64 {
	if v, ok := c.float64s[name]; ok && v != nil {
		return *v
	}
	return 0
}
func (c *Context) Duration(name string) time.Duration {
	if v, ok := c.durations[name]; ok && v != nil {
		return *v
	}
	return 0
}

type StringFlag struct{ Name, Value, Usage string }

func (f *StringFlag) apply(fs *flag.FlagSet, c *Context) {
	c.strings[f.Name] = fs.String(f.Name, f.Value, f.Usage)
}

type IntFlag struct {
	Name, Usage string
	Value       int
}

func (f *IntFlag) apply(fs *flag.FlagSet, c *Context) {
	c.ints[f.Name] = fs.Int(f.Name, f.Value, f.Usage)
}

type Float64Flag struct {
	Name, Usage string
	Value       float64
}

func (f *Float64Flag) apply(fs *flag.FlagSet, c *Context) {
	c.float64s[f.Name] = fs.Float64(f.Name, f.Value, f.Usage)
}

type DurationFlag struct {
	Name, Usage string
	Value       time.Duration
}

func (f *DurationFlag) apply(fs *flag.FlagSet, c *Context) {
	value := durationValue{value: f.Value}
	fs.Var(&value, f.Name, f.Usage)
	c.durations[f.Name] = &value.value
}

type durationValue struct{ value time.Duration }

func (d *durationValue) String() string { return d.value.String() }
func (d *durationValue) Set(s string) error {
	v, err := time.ParseDuration(s)
	if err == nil {
		d.value = v
		return nil
	}
	seconds, convErr := strconv.Atoi(s)
	if convErr != nil {
		return err
	}
	d.value = time.Duration(seconds) * time.Second
	return nil
}
