package anvil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/tadel328/anvil/logger"
)

type Module interface {
	Name() string
	Start(ctx context.Context, app *App) error
	Stop(ctx context.Context) error
}

type Configurable interface {
	Config() any
}

type EnvPrefixer interface {
	EnvPrefix() string
}

type settings struct {
	Env         string        `env:"ANVIL_ENV" default:"production" help:"name of the deployment environment, free form"`
	StopTimeout time.Duration `env:"ANVIL_STOP_TIMEOUT" default:"30s" help:"time given to the modules to stop"`
}

type App struct {
	name     string
	modules  []Module
	byName   map[string]Module
	settings settings
	fields   []envField
	logger   logger.Logger
	envFile  string
	env      string
	lookup   func(string) (string, bool)
	err      error

	mu      sync.Mutex
	started []Module

	done         chan struct{}
	shutdownOnce sync.Once
	cause        error
}

type Option func(*App)

func With(modules ...Module) Option {
	return func(a *App) {
		a.modules = append(a.modules, modules...)
	}
}

func WithLogger(l logger.Logger) Option {
	return func(a *App) {
		a.logger = l
	}
}

func WithEnvFile(path string) Option {
	return func(a *App) {
		a.envFile = path
	}
}

func WithEnv(env string) Option {
	return func(a *App) {
		a.env = env
	}
}

func WithLookup(lookup func(string) (string, bool)) Option {
	return func(a *App) {
		a.lookup = lookup
	}
}

func New(name string, opts ...Option) (*App, error) {
	a := &App{
		name:   name,
		byName: make(map[string]Module),
		done:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(a)
	}

	if a.lookup == nil {
		a.lookup = os.LookupEnv
	}

	a.configure()

	if a.logger == nil {
		return nil, errors.New("logger cannot be nil")
	}

	return a, nil
}

func (a *App) configure() {
	var errs []error
	if a.name == "" {
		errs = append(errs, errors.New("app name is empty"))
	}

	if a.envFile != "" {
		data, err := os.ReadFile(a.envFile)
		switch {
		case err == nil:
			file, err := parseDotenv(string(data))
			if err != nil {
				errs = append(errs, fmt.Errorf("env file %s: %w", a.envFile, err))
				break
			}

			base := a.lookup
			a.lookup = func(key string) (string, bool) {
				if v, ok := base(key); ok {
					return v, true
				}
				v, ok := file[key]
				return v, ok
			}
		case !errors.Is(err, os.ErrNotExist):
			errs = append(errs, fmt.Errorf("env file %s: %w", a.envFile, err))
		}
	}

	fields, err := collectFields("anvil", &a.settings, "")
	if err != nil {
		errs = append(errs, err)
	}
	a.fields = fields

	owners := make(map[string]string, len(fields))
	for _, f := range fields {
		owners[f.key] = f.owner
	}

	for i, m := range a.modules {
		if m == nil {
			errs = append(errs, fmt.Errorf("module #%d is nil", i))
			continue
		}

		name := m.Name()
		switch {
		case name == "":
			errs = append(errs, fmt.Errorf("module #%d (%T) has an empty name", i, m))
			continue
		case a.byName[name] != nil:
			errs = append(errs, fmt.Errorf("module %q is registered twice", name))
			continue
		}
		a.byName[name] = m

		c, ok := m.(Configurable)
		if !ok {
			continue
		}

		prefix := ""
		if p, ok := m.(EnvPrefixer); ok {
			prefix = p.EnvPrefix()
		}

		fields, err := collectFields(name, c.Config(), prefix)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		for _, f := range fields {
			if owner, taken := owners[f.key]; taken {
				errs = append(errs, fmt.Errorf("%s: %s is already read by %s (give the module an env prefix)", name, f.key, owner))
				continue
			}
			owners[f.key] = name
			a.fields = append(a.fields, f)
		}
	}

	var order []string
	byOwner := make(map[string][]envField)
	for _, f := range a.fields {
		if _, seen := byOwner[f.owner]; !seen {
			order = append(order, f.owner)
		}
		byOwner[f.owner] = append(byOwner[f.owner], f)
	}

	for _, owner := range order {
		if err := loadFields(byOwner[owner], a.lookup); err != nil {
			errs = append(errs, err)
		}
	}

	if a.env != "" {
		a.settings.Env = a.env
	}

	if a.settings.StopTimeout <= 0 {
		a.settings.StopTimeout = 30 * time.Second
	}

	a.err = errors.Join(errs...)
}

func (a *App) Name() string { return a.name }

func (a *App) Env() string { return a.settings.Env }

func (a *App) Err() error { return a.err }

func (a *App) Logger(name string) logger.Logger {
	return a.logger.With("logger", name)
}

func (a *App) Start(ctx context.Context) error {
	if err := a.err; err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started != nil {
		return errors.New("app is already started")
	}

	a.logger.WithFields(logger.Fields{"app": a.name, "env": a.settings.Env}).Info("starting")

	started := make([]Module, 0, len(a.modules))
	for _, m := range a.modules {
		a.logger.Debugf("starting module %s", m.Name())

		if err := m.Start(ctx, a); err != nil {
			startErr := fmt.Errorf("start %s: %w", m.Name(), err)

			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.settings.StopTimeout)
			defer cancel()

			return errors.Join(startErr, a.stopModules(stopCtx, started))
		}

		started = append(started, m)
	}

	a.started = started

	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started == nil {
		return nil
	}

	a.logger.With("app", a.name).Info("stopping")

	err := a.stopModules(ctx, a.started)
	a.started = nil

	return err
}

func (a *App) stopModules(ctx context.Context, modules []Module) error {
	var errs []error

	for i := len(modules) - 1; i >= 0; i-- {
		m := modules[i]
		a.logger.Debugf("stopping module %s", m.Name())

		if err := m.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", m.Name(), err))
		}
	}

	return errors.Join(errs...)
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}

	return a.Wait(ctx)
}

func (a *App) Wait(ctx context.Context) error {
	sigCtx, release := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

	var cause error
	select {
	case <-sigCtx.Done():
	case <-a.done:
		cause = a.cause
	}

	release()

	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.settings.StopTimeout)
	defer cancel()

	return errors.Join(cause, a.Stop(stopCtx))
}

func (a *App) Shutdown(cause error) {
	a.shutdownOnce.Do(func() {
		a.cause = cause
		close(a.done)
	})
}

func Use[T any](a *App) (T, bool) {
	for _, m := range a.modules {
		if v, ok := m.(T); ok {
			return v, true
		}
	}

	var zero T
	return zero, false
}

func MustUse[T any](a *App) T {
	v, ok := Use[T](a)
	if !ok {
		var zero T
		panic(fmt.Sprintf("anvil: no module of type %T in app %q", &zero, a.name))
	}

	return v
}

func UseNamed[T any](a *App, name string) (T, bool) {
	v, ok := a.byName[name].(T)
	return v, ok
}
