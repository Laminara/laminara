package diag

import (
	"context"
	"fmt"
)

type Verdict int

const (
	Skipped Verdict = iota
	OK
	Warn
	Fail
)

type Remedy struct {
	Hint    string
	Command string
	Apply   func(ctx context.Context) error
}

type Result struct {
	Section string
	What    string
	Verdict Verdict
	Detail  string
	Remedy  Remedy
}

type Probe struct {
	section string
	results []Result
}

func New(section string) *Probe {
	return &Probe{section: section}
}

func (p *Probe) Section() string {
	return p.section
}

func (p *Probe) Results() []Result {
	return p.results
}

func (p *Probe) OK(what, format string, args ...any) {
	p.add(OK, what, fmt.Sprintf(format, args...), Remedy{})
}

func (p *Probe) Skip(what, format string, args ...any) {
	p.add(Skipped, what, fmt.Sprintf(format, args...), Remedy{})
}

func (p *Probe) Warn(what, detail string, remedy Remedy) {
	p.add(Warn, what, detail, remedy)
}

func (p *Probe) Fail(what, detail string, remedy Remedy) {
	p.add(Fail, what, detail, remedy)
}

func (p *Probe) Add(result Result) {
	if result.Section == "" {
		result.Section = p.section
	}
	p.results = append(p.results, result)
}

func (p *Probe) Absorb(other *Probe) {
	p.results = append(p.results, other.results...)
}

func (p *Probe) add(verdict Verdict, what, detail string, remedy Remedy) {
	p.results = append(p.results, Result{
		Section: p.section,
		What:    what,
		Verdict: verdict,
		Detail:  detail,
		Remedy:  remedy,
	})
}

type Checker interface {
	Check(ctx context.Context, p *Probe)
}

func Worst(results []Result) Verdict {
	worst := OK
	for _, result := range results {
		if result.Verdict > worst {
			worst = result.Verdict
		}
	}
	return worst
}

func Fixable(results []Result) int {
	count := 0
	for _, result := range results {
		if result.Remedy.Apply != nil {
			count++
		}
	}
	return count
}

func Tally(results []Result) (ok, warn, fail, skipped int) {
	for _, result := range results {
		switch result.Verdict {
		case OK:
			ok++
		case Warn:
			warn++
		case Fail:
			fail++
		case Skipped:
			skipped++
		}
	}
	return ok, warn, fail, skipped
}
