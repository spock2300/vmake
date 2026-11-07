package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

type projectOptions struct {
	execution     context.Context
	policy        *resolver.LockPolicy
	pins          map[string]string
	overrides     []inspectOverride
	softGitignore bool
}

type projectResolveError struct {
	ctx *RuntimeContext
	err error
}

func (e *projectResolveError) Error() string { return e.err.Error() }
func (e *projectResolveError) Unwrap() error { return e.err }

type optionOverrideError struct {
	err error
}

func (e *optionOverrideError) Error() string { return e.err.Error() }
func (e *optionOverrideError) Unwrap() error { return e.err }

func openProject(opts projectOptions) (*RuntimeContext, error) {
	overrides := opts.overrides
	if overrides == nil {
		var err error
		overrides, err = parseInspectOverrides(ephemeralSetFlags)
		if err != nil {
			return nil, err
		}
	}
	locks := commandStorageLocks()
	ignoreLock := opts.policy != nil && opts.policy.IgnoreAll
	params, err := loadResolveParams(ignoreLock, opts.softGitignore)
	if err != nil {
		return nil, err
	}
	if opts.pins != nil {
		params.Pins = opts.pins
	}
	ctx := newRuntimeContext(params, locks)
	ctx.Context = opts.execution
	if ctx.Context == nil {
		ctx.Context = context.Background()
	}
	if opts.policy != nil {
		ctx.LockPolicy = *opts.policy
	} else {
		if lockedFlag {
			ctx.LockPolicy.StrictWrapper = true
			ctx.LockPolicy.Locked = true
		}
		if offlineFlag {
			ctx.LockPolicy.Offline = true
		}
	}
	var overrideErr error
	if len(overrides) > 0 {
		ctx.OnOptionsCollected = func(c *RuntimeContext) error {
			overrideErr = applyEphemeralOverrides(c, overrides)
			return overrideErr
		}
	}
	if err := pipeline.Require(ctx); err != nil {
		closeProject(ctx)
		return nil, &projectResolveError{ctx: ctx, err: err}
	}
	if err := pipeline.Configure(ctx); err != nil {
		closeProject(ctx)
		if overrideErr != nil {
			return nil, &optionOverrideError{err: overrideErr}
		}
		return nil, err
	}
	pipeline.ResolveActiveChipForDisplay(ctx)
	return ctx, nil
}

func openProjectBestEffort(opts projectOptions) (*RuntimeContext, bool) {
	ctx, err := openProject(opts)
	if err != nil {
		var resolveErr *projectResolveError
		if errors.As(err, &resolveErr) {
			return resolveErr.ctx, false
		}
		fatalErr(err)
	}
	return ctx, true
}

func mustOpenProject(opts projectOptions) *RuntimeContext {
	ctx, err := openProject(opts)
	fatalErr(err)
	return ctx
}

func closeProject(ctx *RuntimeContext) {
	if ctx != nil && ctx.Resolver != nil {
		ctx.Resolver.Close()
		ctx.Resolver = nil
	}
}

func applyEphemeralOverrides(ctx *RuntimeContext, overrides []inspectOverride) error {
	for i := range overrides {
		override := &overrides[i]
		override.pkgName = resolveSetPackageName(ctx, override.pkgName)
		opt, err := inspectOverrideOption(ctx, override.pkgName, override.optName)
		if err != nil {
			return err
		}
		value, err := coerceValue(override.raw, opt.Type())
		if err != nil {
			return fmt.Errorf("%s: %w", override.key(), err)
		}
		override.value = value
		if err := validateInspectOverride(ctx, *override); err != nil {
			return err
		}
		applySetValue(ctx, override.pkgName, override.optName, value)
	}
	return nil
}
